use crate::{AdapterError, ValidatedRequest};
use std::ffi::c_void;
use std::ptr::NonNull;
use std::slice;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc, Condvar, Mutex,
};
use std::thread;
use std::time::Duration;

unsafe extern "C" {
    fn PlatformCpSatNewEnv() -> *mut c_void;
    fn PlatformCpSatDestroyEnv(environment: *mut c_void);
    fn PlatformCpSatStopSearch(environment: *mut c_void);
    fn PlatformCpSatSolve(
        environment: *mut c_void,
        model: *const u8,
        model_length: usize,
        parameters: *const u8,
        parameters_length: usize,
        maximum_response_length: usize,
        response: *mut *mut u8,
        response_length: *mut usize,
    ) -> i32;
    fn PlatformCpSatFreeResponse(response: *mut u8);
}

struct EnvironmentState {
    pointer: NonNull<c_void>,
    solving: bool,
}

// The upstream environment may be interrupted concurrently, but destruction
// must never race a stop request. The mutex mirrors the official Go binding's
// one-environment-per-solve ownership and stop/delete serialization.
struct Environment(Mutex<EnvironmentState>);

// SAFETY: the environment pointer is accessed only while holding the mutex;
// upstream explicitly supports SolveCpStopSearch concurrent with solve.
unsafe impl Send for Environment {}
unsafe impl Sync for Environment {}

impl Drop for Environment {
    fn drop(&mut self) {
        let state = self
            .0
            .get_mut()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        // SAFETY: the constructor owns exactly one non-null native environment,
        // and the solve thread has returned before Environment is dropped.
        unsafe { PlatformCpSatDestroyEnv(state.pointer.as_ptr()) };
    }
}

impl Environment {
    fn new() -> Result<Self, AdapterError> {
        // SAFETY: construction has no arguments and returns sole ownership or null.
        NonNull::new(unsafe { PlatformCpSatNewEnv() })
            .map(|pointer| Self(Mutex::new(EnvironmentState { pointer, solving: false })))
            .ok_or(AdapterError::SolverFailure)
    }

    fn pointer_for_solve(&self) -> *mut c_void {
        let mut state = self
            .0
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        state.solving = true;
        state.pointer.as_ptr()
    }

    fn finish_solve(&self) {
        let mut state = self
            .0
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        state.solving = false;
    }

    fn stop_search(&self) {
        let state = self
            .0
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.solving {
            // SAFETY: the solve owns this environment until it joins the watcher,
            // and upstream explicitly permits concurrent stop during a solve.
            unsafe { PlatformCpSatStopSearch(state.pointer.as_ptr()) };
        }
    }
}

struct Response(*mut u8);

impl Drop for Response {
    fn drop(&mut self) {
        if !self.0.is_null() {
            // SAFETY: PlatformCpSatSolve is the only source of this allocation.
            unsafe { PlatformCpSatFreeResponse(self.0) };
        }
    }
}

pub fn solve(request: &ValidatedRequest) -> Result<(Vec<u8>, bool), AdapterError> {
    let environment = Arc::new(Environment::new()?);
    let completion = Arc::new((Mutex::new(false), Condvar::new()));
    let deadline_triggered = Arc::new(AtomicBool::new(false));
    let watcher_completion = Arc::clone(&completion);
    let watcher_triggered = Arc::clone(&deadline_triggered);
    let watcher_environment = Arc::clone(&environment);
    let watchdog = Duration::from_millis(request.watchdog_milliseconds() as u64);
    let watcher = thread::spawn(move || {
        let (lock, condition) = &*watcher_completion;
        let completed = lock.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        let (completed, timed_out) = condition
            .wait_timeout_while(completed, watchdog, |done| !*done)
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if timed_out.timed_out() && !*completed {
            watcher_triggered.store(true, Ordering::SeqCst);
            watcher_environment.stop_search();
        }
    });

    let mut response_pointer = std::ptr::null_mut();
    let mut response_length = 0usize;
    let environment_pointer = environment.pointer_for_solve();
    // Close the race where the watcher expires immediately before FFI entry.
    if deadline_triggered.load(Ordering::SeqCst) {
        environment.stop_search();
    }
    // SAFETY: all input slices remain alive for the call; output pointers are
    // initialized and released only with the paired bridge free function.
    let code = unsafe {
        PlatformCpSatSolve(
            environment_pointer,
            request.model().as_ptr(),
            request.model().len(),
            request.parameters().as_ptr(),
            request.parameters().len(),
            request.maximum_response_bytes(),
            &mut response_pointer,
            &mut response_length,
        )
    };
    environment.finish_solve();
    {
        let (lock, condition) = &*completion;
        let mut completed = lock.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        *completed = true;
        condition.notify_one();
    }
    watcher.join().map_err(|_| AdapterError::SolverFailure)?;
    let response = Response(response_pointer);
    if code != 0
        || response.0.is_null()
        || response_length == 0
        || response_length > request.maximum_response_bytes()
    {
        return Err(AdapterError::SolverFailure);
    }
    // SAFETY: the bridge returned response_length initialized bytes owned by
    // response, which remains alive until after this copy.
    let copied = unsafe { slice::from_raw_parts(response.0, response_length) }.to_vec();
    Ok((copied, deadline_triggered.load(Ordering::SeqCst)))
}
