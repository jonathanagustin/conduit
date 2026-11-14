# CONDUIT-INVENTED: packaging.CodexRuntimeProbe
# Displaced-by: A reviewed, scoped, authenticated agent runtime contract with enforced workspace isolation.
"""Handshake-only Codex0.148.0 capability probe. Never starts a thread or turn.
The pinned protocol lacks restricted read roots; this is NOT an agent runtime.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import time

VERSION = "codex-cli 0.148.0"
# Official @openai/codex-win32-x64@0.148.0 acquisition; other builds require review.
BINARY_SHA = "2ad2cf8a732da68b8f141634f92db1a03016c5faf533a7225fbc0fb740130410"
MAX_LINE = 64 * 1024
TIMEOUT = 15

def digest(path):
    h=hashlib.sha256()
    with path.open("rb") as f:
        for data in iter(lambda:f.read(1024*1024),b""):h.update(data)
    return h.hexdigest()

def unique(pairs):
    obj={}
    for k,v in pairs:
        if k in obj:raise ValueError("duplicate protocol key")
        obj[k]=v
    return obj

def verify_response(raw,home):
    if len(raw)>MAX_LINE:raise ValueError("protocol message exceeds limit")
    msg=json.loads(raw,object_pairs_hook=unique)
    if not isinstance(msg,dict) or "method" in msg or "error" in msg or type(msg.get("id")) is not int or msg["id"]!=1:
        raise ValueError("unexpected protocol response or server request")
    result=msg.get("result")
    if not isinstance(result,dict):raise ValueError("initialization result missing")
    for name in ["codexHome","platformFamily","platformOs","userAgent"]:
        if not isinstance(result.get(name),str) or not result[name] or len(result[name])>4096:raise ValueError("invalid initialization field")
    if os.path.normcase(str(Path(result["codexHome"]).resolve()))!=os.path.normcase(str(home.resolve())):
        raise ValueError("server did not use isolated home")
    if result["platformFamily"] not in ["windows","unix"] or result["platformOs"] not in ["windows","linux","macos"]:
        raise ValueError("unsupported runtime platform")
    return result["platformOs"]

def verify_followup(raw):
    msg=json.loads(raw,object_pairs_hook=unique)
    if not isinstance(msg,dict) or "id" in msg or "error" in msg or msg.get("method")!="remoteControl/status/changed":
        raise ValueError("unexpected server request or notification")
    params=msg.get("params")
    if not isinstance(params,dict) or params.get("status")!="disabled" or params.get("environmentId") is not None:
        raise ValueError("remote control must remain disabled")
    for key in ["installationId","serverName"]:
        if not isinstance(params.get(key),str) or len(params[key])>4096:
            raise ValueError("invalid status notification")

def probe(executable,expected):
    receipt={"status":"blocked","runtimeAvailable":False,"threadStarted":False,"turnStarted":False,"profile":"codex-handshake-only/v1"}
    process=None;readers=[];temporary=None
    try:
        if expected!=BINARY_SHA or digest(executable)!=expected:raise ValueError("executable digest mismatch")
        temporary=tempfile.TemporaryDirectory(prefix="conduit-codex-probe-")
        temp=temporary.name
        root=Path(temp);home=root/'home';workspace=root/'workspace';home.mkdir();workspace.mkdir();(home/"appdata").mkdir();(home/"local").mkdir()
        env={k:v for k,v in os.environ.items() if k.upper() in {'PATH','SYSTEMROOT','WINDIR','TEMP','TMP'}}
        env.update(CODEX_HOME=str(home),HOME=str(home),USERPROFILE=str(home),APPDATA=str(home/'appdata'),LOCALAPPDATA=str(home/'local'))
        process=subprocess.Popen([str(executable),'app-server','--listen','stdio://'],cwd=workspace,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        messages=queue.Queue(maxsize=4);failed=threading.Event()
        def stop(pipe):
            receipt["failureStage"]=pipe+"-capture"
            failed.set()
            try:process.kill()
            except OSError:pass
        def stdout():
            try:
                total=0
                while True:
                    data=process.stdout.readline(MAX_LINE+1)
                    if not data:break
                    total+=len(data)
                    if len(data)>MAX_LINE or total>4*MAX_LINE or not data.endswith(b'\n'):raise ValueError('invalid protocol frame')
                    messages.put_nowait(data)
            except BaseException:stop("stdout")
        def stderr():
            total=0
            try:
                while True:
                    data=process.stderr.read(4096)
                    if not data:break
                    total+=len(data)
                    if total>32*1024:raise ValueError('stderr exceeds limit')
            except BaseException:stop("stderr")
        readers=[threading.Thread(target=stdout,daemon=True),threading.Thread(target=stderr,daemon=True)]
        for reader in readers:reader.start()
        initialize={'id':1,'method':'initialize','params':{'clientInfo':{'name':'conduit_runtime_probe','version':'0.1.0'},'capabilities':{'experimentalApi':False}}}
        process.stdin.write((json.dumps(initialize)+'\n').encode());process.stdin.flush()
        deadline=time.monotonic()+TIMEOUT
        while True:
            if failed.is_set():raise RuntimeError('protocol capture failed')
            try:raw=messages.get(timeout=min(.1,max(.001,deadline-time.monotonic())));break
            except queue.Empty:
                if time.monotonic()>=deadline:raise TimeoutError('initialization timed out')
        platform=verify_response(raw,home)
        process.stdin.write(b'{"method":"initialized","params":{}}\n');process.stdin.flush()
        process.stdin.close()
        try:
            code=process.wait(timeout=3)
            if code!=0:
                receipt["failureStage"]="server-exit";receipt["exitCode"]=code
                raise RuntimeError("server exited unsuccessfully")
        except subprocess.TimeoutExpired:
            process.kill();process.wait(timeout=3)
            raise TimeoutError("server did not close after handshake")
        for reader in readers:reader.join(timeout=1)
        if failed.is_set() or any(r.is_alive() for r in readers):raise RuntimeError('protocol capture did not complete')
        while not messages.empty():
            verify_followup(messages.get_nowait())
            receipt['remoteControl']='disabled'
        if digest(executable)!=expected:raise ValueError('executable changed during probe')
        receipt.update(status='handshake-verified-model-disabled',codexVersion='0.148.0',executableSHA256=expected,platform=platform,
            methods=['initialize','initialized'],blockers=['Pinned0.148.0 readOnly sandbox has no restricted-readable-roots field.','No independently confined inference environment or dedicated authentication was established.'])
    except Exception as exc:
        receipt['reason']=type(exc).__name__
    finally:
        cleanup_failed=False
        exited=process is None
        if process is not None:
            try:
                if process.poll() is None:process.kill()
                process.wait(timeout=3)
                exited=True
            except Exception:
                cleanup_failed=True
            if exited:
                for pipe in [process.stdin,process.stdout,process.stderr]:
                    if pipe is not None:
                        try:pipe.close()
                        except Exception:cleanup_failed=True
            for reader in readers:reader.join(timeout=1)
            if any(reader.is_alive() for reader in readers):cleanup_failed=True
        if temporary is not None and exited:
            try:temporary.cleanup()
            except OSError:cleanup_failed=True
        if cleanup_failed:receipt.update(status="blocked",reason="ProcessOrTemporaryCleanupFailed")
    return receipt

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--executable',type=Path,required=True)
    p.add_argument('--sha256',required=True,help='digest from trusted official acquisition, not arbitrary executable self-assertion')
    a=p.parse_args();result=probe(a.executable,a.sha256);print(json.dumps(result,separators=(',',':')))
    return 0 if result['status']=='handshake-verified-model-disabled' else 1
if __name__=='__main__':raise SystemExit(main())
