"""Protocol boundary tests only; no model, authentication or runtime execution."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
spec=importlib.util.spec_from_file_location('probe',Path(__file__).with_name('probe-codex-runtime.py'))
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)
class ProtocolTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.home=Path(self.temp.name)
  self.message={'id':1,'result':{'codexHome':str(self.home),'platformFamily':'windows','platformOs':'windows','userAgent':'fixture'}}
 def test_matching_handshake(self):self.assertEqual(p.verify_response(json.dumps(self.message).encode(),self.home),'windows')
 def test_server_requests_never_approved(self):
  for method in ['item/commandExecution/requestApproval','item/permissions/requestApproval','unknown']:
   with self.assertRaises(ValueError):p.verify_response(json.dumps({'id':1,'method':method,'params':{}}).encode(),self.home)
 def test_wrong_id_home_error_or_fields_fail_closed(self):
  for change in [lambda m:m.update(id=2),lambda m:m.update(id=True),lambda m:m.update(error={}),lambda m:m['result'].update(codexHome=str(self.home/'other')),lambda m:m['result'].pop('userAgent')]:
   m=json.loads(json.dumps(self.message));change(m)
   with self.assertRaises(ValueError):p.verify_response(json.dumps(m).encode(),self.home)
 def test_duplicate_malformed_and_oversized_frames(self):
  for data in [b'{"id":1,"id":2}',b'{',b'x'*(p.MAX_LINE+1),b'null']:
   with self.assertRaises((ValueError,TypeError)):p.verify_response(data,self.home)


class ProcessBoundaryTests(unittest.TestCase):
 def test_probe_sends_only_handshake_and_excludes_credentials(self):
  import io,os,types
  from unittest.mock import patch
  record={}
  class Input(io.BytesIO):
   def close(self):
    if not self.closed:record['sent']=self.getvalue()
    super().close()
  class Process:
   def __init__(self,*args,**kwargs):
    record['env']=kwargs['env'];record['args']=args[0]
    response={'id':1,'result':{'codexHome':kwargs['env']['CODEX_HOME'],'platformFamily':'windows','platformOs':'windows','userAgent':'fixture'}}
    self.stdin=Input();self.stdout=io.BytesIO((json.dumps(response)+'\n').encode());self.stderr=io.BytesIO(b'')
   def wait(self,timeout=None):return 0
   def poll(self):return 0
   def kill(self):pass
  with tempfile.TemporaryDirectory() as d:
   exe=Path(d)/'codex-fixture';exe.write_bytes(b'fixture never executed')
   with patch.dict(os.environ,{'OPENAI_API_KEY':'fixture-do-not-inherit','CODEX_HOME':'personal-fixture','HELM_KUBETOKEN':'fixture-do-not-inherit'}),patch.object(p,'digest',return_value=p.BINARY_SHA),patch.object(p.subprocess,'Popen',Process):
    result=p.probe(exe,p.BINARY_SHA)
   self.assertEqual(result['status'],'handshake-verified-model-disabled')
   self.assertFalse(result['runtimeAvailable']);self.assertFalse(result['turnStarted'])
   methods=[json.loads(line)['method'] for line in record['sent'].splitlines()]
   self.assertEqual(methods,['initialize','initialized'])
   self.assertNotIn('OPENAI_API_KEY',record['env']);self.assertNotIn('HELM_KUBETOKEN',record['env'])
   self.assertNotEqual(record['env']['CODEX_HOME'],'personal-fixture')
 def test_digest_failure_never_starts_process(self):
  from unittest.mock import patch
  with tempfile.TemporaryDirectory() as d:
   exe=Path(d)/'fixture';exe.write_bytes(b'x')
   with patch.object(p.subprocess,'Popen') as process:
    result=p.probe(exe,'a'*64)
    self.assertEqual(result['status'],'blocked');process.assert_not_called()



class ShutdownBoundaryTests(unittest.TestCase):
 def test_extra_requests_exit_failure_timeout_overflow_and_cleanup_fail(self):
  import io,subprocess
  from unittest.mock import patch
  for scenario in ['extra','nonzero','timeout','overflow','cleanup']:
   class Stream(io.BytesIO):
    def close(self):
     super().close()
     if scenario=='cleanup':raise OSError('fixture cleanup failure')
   class Process:
    def __init__(self,*args,**kwargs):
     self.alive=True;self.calls=0
     response={'id':1,'result':{'codexHome':kwargs['env']['CODEX_HOME'],'platformFamily':'windows','platformOs':'windows','userAgent':'fixture'}}
     data=(json.dumps(response)+'\n').encode()
     if scenario=='extra':data+=b'{"id":99,"method":"item/permissions/requestApproval","params":{}}\n'
     if scenario=='overflow':data+=b'x'*(p.MAX_LINE+1)
     self.stdin=Stream();self.stdout=Stream(data);self.stderr=Stream()
    def wait(self,timeout=None):
     self.calls+=1
     if scenario=='timeout' and self.calls==1:raise subprocess.TimeoutExpired('fixture',3)
     self.alive=False;return 1 if scenario=='nonzero' else 0
    def poll(self):return None if self.alive else 0
    def kill(self):self.alive=False
   with tempfile.TemporaryDirectory() as d:
    exe=Path(d)/'fixture';exe.write_bytes(b'never executed')
    with patch.object(p,'digest',return_value=p.BINARY_SHA),patch.object(p.subprocess,'Popen',Process):
     result=p.probe(exe,p.BINARY_SHA)
    self.assertEqual(result['status'],'blocked',scenario)
    self.assertFalse(result['runtimeAvailable'])



class StatusNotificationTests(unittest.TestCase):
 def test_only_disabled_remote_control_notification_allowed(self):
  message={'method':'remoteControl/status/changed','params':{'status':'disabled','installationId':'fixture','serverName':'fixture'}}
  p.verify_followup(json.dumps(message))
  for change in [lambda m:m.update(id=3),lambda m:m['params'].update(status='connected'),lambda m:m['params'].update(environmentId='remote'),lambda m:m.update(method='unknown')]:
   m=json.loads(json.dumps(message));change(m)
   with self.assertRaises(ValueError):p.verify_followup(json.dumps(m))

if __name__=='__main__':unittest.main()
