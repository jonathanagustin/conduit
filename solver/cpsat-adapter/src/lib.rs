use base64::{engine::general_purpose::STANDARD, Engine as _};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fmt::{Display, Formatter};
use std::io::{Read, Write};

pub const API_VERSION: &str =
    "optimization.platform.example/external-discrete-solver-process/v1alpha1";
pub const MAXIMUM_INPUT_BYTES: u64 = 12 << 20;
pub const MAXIMUM_MODEL_BYTES: usize = 8 << 20;
pub const MAXIMUM_PARAMETERS_BYTES: usize = 64 << 10;
pub const MAXIMUM_RESPONSE_BYTES: usize = 4 << 20;
const MAXIMUM_WATCHDOG_MILLISECONDS: i64 = 24 * 60 * 60 * 1_000;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct ProcessRequest {
    api_version: String,
    kind: String,
    request_id: String,
    model_digest: String,
    parameters_digest: String,
    model_base64: String,
    parameters_base64: String,
    watchdog_milliseconds: i64,
    maximum_response_bytes: usize,
}

#[derive(Debug)]
pub struct ValidatedRequest {
    request_id: String,
    model_digest: String,
    parameters_digest: String,
    model: Vec<u8>,
    parameters: Vec<u8>,
    watchdog_milliseconds: i64,
    maximum_response_bytes: usize,
}

impl ValidatedRequest {
    pub fn model(&self) -> &[u8] {
        &self.model
    }

    pub fn parameters(&self) -> &[u8] {
        &self.parameters
    }

    pub fn watchdog_milliseconds(&self) -> i64 {
        self.watchdog_milliseconds
    }

    pub fn maximum_response_bytes(&self) -> usize {
        self.maximum_response_bytes
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessResponse<'a> {
    api_version: &'static str,
    kind: &'static str,
    request_id: &'a str,
    model_digest: &'a str,
    parameters_digest: &'a str,
    response_base64: String,
    deadline_triggered: bool,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AdapterError {
    InvalidInput,
    InvalidOutput,
    SolverFailure,
}

impl Display for AdapterError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        let message = match self {
            Self::InvalidInput => "invalid bounded CP-SAT request",
            Self::InvalidOutput => "invalid bounded CP-SAT response",
            Self::SolverFailure => "CP-SAT native bridge failed",
        };
        formatter.write_str(message)
    }
}

impl std::error::Error for AdapterError {}

pub fn run<R, W, S>(reader: R, mut writer: W, solver: S) -> Result<(), AdapterError>
where
    R: Read,
    W: Write,
    S: FnOnce(&ValidatedRequest) -> Result<(Vec<u8>, bool), AdapterError>,
{
    let request = read_request(reader)?;
    let (response, deadline_triggered) = solver(&request)?;
    if response.is_empty()
        || response.len() > request.maximum_response_bytes
        || response.len() > MAXIMUM_RESPONSE_BYTES
    {
        return Err(AdapterError::InvalidOutput);
    }
    let envelope = ProcessResponse {
        api_version: API_VERSION,
        kind: "ExternalDiscreteSolverProcessResponse",
        request_id: &request.request_id,
        model_digest: &request.model_digest,
        parameters_digest: &request.parameters_digest,
        response_base64: STANDARD.encode(response),
        deadline_triggered,
    };
    serde_json::to_writer(&mut writer, &envelope).map_err(|_| AdapterError::InvalidOutput)?;
    writer
        .write_all(b"\n")
        .map_err(|_| AdapterError::InvalidOutput)
}

fn read_request<R: Read>(reader: R) -> Result<ValidatedRequest, AdapterError> {
    let mut encoded = Vec::new();
    reader
        .take(MAXIMUM_INPUT_BYTES + 1)
        .read_to_end(&mut encoded)
        .map_err(|_| AdapterError::InvalidInput)?;
    if encoded.is_empty() || encoded.len() as u64 > MAXIMUM_INPUT_BYTES {
        return Err(AdapterError::InvalidInput);
    }
    let request: ProcessRequest =
        serde_json::from_slice(&encoded).map_err(|_| AdapterError::InvalidInput)?;
    if request.api_version != API_VERSION
        || request.kind != "ExternalDiscreteSolverProcessRequest"
        || !valid_content_id(&request.request_id, "dsolreq_")
        || !valid_digest(&request.model_digest)
        || !valid_digest(&request.parameters_digest)
        || request.watchdog_milliseconds < 1
        || request.watchdog_milliseconds > MAXIMUM_WATCHDOG_MILLISECONDS
        || request.maximum_response_bytes < 1
        || request.maximum_response_bytes > MAXIMUM_RESPONSE_BYTES
    {
        return Err(AdapterError::InvalidInput);
    }
    let model = decode_bounded(&request.model_base64, MAXIMUM_MODEL_BYTES)?;
    let parameters = decode_bounded(&request.parameters_base64, MAXIMUM_PARAMETERS_BYTES)?;
    if digest(&model) != request.model_digest || digest(&parameters) != request.parameters_digest {
        return Err(AdapterError::InvalidInput);
    }
    Ok(ValidatedRequest {
        request_id: request.request_id,
        model_digest: request.model_digest,
        parameters_digest: request.parameters_digest,
        model,
        parameters,
        watchdog_milliseconds: request.watchdog_milliseconds,
        maximum_response_bytes: request.maximum_response_bytes,
    })
}

fn decode_bounded(encoded: &str, maximum: usize) -> Result<Vec<u8>, AdapterError> {
    if encoded.is_empty() || encoded.len() > maximum.div_ceil(3) * 4 {
        return Err(AdapterError::InvalidInput);
    }
    let value = STANDARD
        .decode(encoded)
        .map_err(|_| AdapterError::InvalidInput)?;
    if value.is_empty() || value.len() > maximum {
        return Err(AdapterError::InvalidInput);
    }
    Ok(value)
}

fn valid_content_id(value: &str, prefix: &str) -> bool {
    value.len() == prefix.len() + 64
        && value.starts_with(prefix)
        && value[prefix.len()..]
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

fn valid_digest(value: &str) -> bool {
    value.len() == 71
        && value.starts_with("sha256:")
        && value[7..]
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

fn digest(value: &[u8]) -> String {
    format!("sha256:{}", hex::encode(Sha256::digest(value)))
}

#[cfg(feature = "native")]
pub mod native;

#[cfg(feature = "native")]
pub fn native_main() -> i32 {
    match run(
        std::io::stdin().lock(),
        std::io::stdout().lock(),
        native::solve,
    ) {
        Ok(()) => 0,
        Err(error) => {
            eprintln!("{error}");
            2
        }
    }
}

#[cfg(feature = "native")]
#[unsafe(no_mangle)]
pub extern "C" fn PlatformCpSatAdapterMain() -> i32 {
    native_main()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{json, Value};

    fn request() -> Value {
        let model = b"model";
        let parameters = b"parameters";
        json!({
            "apiVersion": API_VERSION,
            "kind": "ExternalDiscreteSolverProcessRequest",
            "requestId": format!("dsolreq_{}", "0".repeat(64)),
            "modelDigest": digest(model),
            "parametersDigest": digest(parameters),
            "modelBase64": STANDARD.encode(model),
            "parametersBase64": STANDARD.encode(parameters),
            "watchdogMilliseconds": 1000,
            "maximumResponseBytes": 1024
        })
    }

    #[test]
    fn binds_input_and_emits_only_raw_response() {
        let encoded = serde_json::to_vec(&request()).unwrap();
        let mut output = Vec::new();
        run(encoded.as_slice(), &mut output, |request| {
            assert_eq!(request.model(), b"model");
            assert_eq!(request.parameters(), b"parameters");
            Ok((vec![8, 4], false))
        })
        .unwrap();
        let response: Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(response["responseBase64"], STANDARD.encode([8, 4]));
        assert!(response.get("result").is_none());
        assert_eq!(response["deadlineTriggered"], false);
    }

    #[test]
    fn rejects_unknown_fields_digest_tampering_and_bounds() {
        let mut cases = Vec::new();
        let mut unknown = request();
        unknown["result"] = json!({"makespanSeconds": 0});
        cases.push(unknown);
        let mut digest_tamper = request();
        digest_tamper["modelDigest"] = Value::String(format!("sha256:{}", "a".repeat(64)));
        cases.push(digest_tamper);
        let mut bad_id = request();
        bad_id["requestId"] = Value::String(format!("dsolreq_{}", "A".repeat(64)));
        cases.push(bad_id);
        let mut bad_watchdog = request();
        bad_watchdog["watchdogMilliseconds"] = json!(0);
        cases.push(bad_watchdog);
        let mut bad_output_bound = request();
        bad_output_bound["maximumResponseBytes"] = json!(MAXIMUM_RESPONSE_BYTES + 1);
        cases.push(bad_output_bound);

        for candidate in cases {
            let encoded = serde_json::to_vec(&candidate).unwrap();
            assert_eq!(
                run(encoded.as_slice(), Vec::new(), |_| Ok((vec![1], false))),
                Err(AdapterError::InvalidInput)
            );
        }
    }

    #[test]
    fn rejects_empty_and_oversized_solver_output() {
        let encoded = serde_json::to_vec(&request()).unwrap();
        assert_eq!(
            run(encoded.as_slice(), Vec::new(), |_| Ok((Vec::new(), false))),
            Err(AdapterError::InvalidOutput)
        );
        assert_eq!(
            run(encoded.as_slice(), Vec::new(), |_| Ok((
                vec![0; 1025],
                true
            ))),
            Err(AdapterError::InvalidOutput)
        );
    }
}
