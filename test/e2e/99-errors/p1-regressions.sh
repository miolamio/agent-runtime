#!/usr/bin/env bash
# Regression coverage for ART-1/5/6/8: exercise the actual CLI in isolated HOMEs.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
export ART_TEST_HOME="$th"
python3 - <<'PY'
import concurrent.futures, json, os, pathlib, subprocess, tempfile
base=pathlib.Path(os.environ['ART_TEST_HOME'])
binary=os.environ['AIRUN_BIN']

def run(home,args=(),**extra):
 env=dict(os.environ,HOME=str(home),PATH=str(base/'bin')+os.pathsep+os.environ['PATH'],**extra)
 return subprocess.run([binary,*args],cwd=home,env=env,input='',capture_output=True,text=True)

for cmd in [('init',),('proxy','init')]:
 with tempfile.TemporaryDirectory(dir=base) as tmp:
  p=run(pathlib.Path(tmp),cmd)
  assert p.returncode==0,(cmd,p.stderr)
  for f in (pathlib.Path(tmp)/'.airun').glob('*'):
   if f.suffix in ('.env','.yaml','.json'): assert f.stat().st_mode&0o777==0o600,f
# A blocked config directory must report failure, not claim initialization succeeded.
with tempfile.TemporaryDirectory(dir=base) as tmp:
 home=pathlib.Path(tmp);(home/'.airun').write_text('blocker')
 for cmd in [('init',),('proxy','init')]: assert run(home,cmd).returncode!=0

config=base/'.airun/config.env'
original=config.read_text()
log=pathlib.Path(os.environ['DOCKER_SHIM_LOG'])
for mode in ('snapsho','SNAPSHOT','unknown'):
 config.write_text(original.replace('ARUN_MODE=bind','ARUN_MODE='+mode))
 for args in [('ping',),('shell',),('--parallel','--agent','one:ping','--agent','two:ping')]:
  log.write_text('');p=run(base,args)
  assert p.returncode!=0 and 'invalid ARUN_MODE' in p.stderr,(mode,args,p.stderr)
  assert not log.read_text(),(mode,log.read_text())

# Both absent and empty modes select snapshot; valid bind/snapshot still work.
for mode in ('',None,'snapshot','bind'):
 replacement='' if mode is None else 'ARUN_MODE='+mode
 config.write_text(original.replace('ARUN_MODE=bind',replacement));log.write_text('')
 p=run(base,('--no-state','ping'));assert p.returncode==0,p.stderr
 expected='run --rm' if mode=='bind' else 'create --name airun-snap-'
 assert expected in log.read_text(),log.read_text()

# Multiple independent CLI processes in each lifecycle keep distinct histories.
for mode,export in [('bind',False),('snapshot',False),('bind',True),('snapshot',True)]:
 config.write_text(original.replace('ARUN_MODE=bind','ARUN_MODE='+mode))
 def launch(i):
  name=f'{mode}-{export}-{i}'
  args=['--no-state','--name',name]
  if export: args+=['--output',str(base/'exports'/name)]
  args+=['prompt-'+name]
  p=run(base,args);assert p.returncode==0,p.stderr
  return name
 with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool: names=list(pool.map(launch,range(24)))
 records=[json.loads(f.read_text()) for f in (base/'.airun/runs').glob('*/meta.json')]
 selected=[r for r in records if r.get('agent_name') in names]
 assert len(selected)==len(names),(len(selected),len(names))
 assert len({r['run_id'] for r in selected})==len(names)
 for r in selected:
  assert r['prompt']=='prompt-'+r['agent_name']
  assert r['run_id'] in pathlib.Path(r['run_dir']).name
  assert (pathlib.Path(r['run_dir'])/'prompt.txt').read_text()==r['prompt']
  assert (pathlib.Path(r['run_dir'])/'output.txt').read_text().strip().endswith(r['run_id'])

config.write_text(original.replace('ARUN_MODE=bind','ARUN_MODE=snapshot'))
for failure in ('cp','mkdir','process'):
 log.write_text('');out=base/('failed-'+failure)
 if failure=='mkdir': out.write_text('blocker')
 extra={'DOCKER_SHIM_FAIL_EXPORT':'1'} if failure=='cp' else {}
 if failure=='process': extra['DOCKER_SHIM_EXIT_CODE']='7'
 p=run(base,('--no-state','--name','failed-'+failure,'--output',str(out),'ping'),**extra)
 assert p.returncode!=0,(failure,p.stdout,p.stderr)
 records=[json.loads(f.read_text()) for f in (base/'.airun/runs').glob('*/meta.json')]
 record=next(r for r in records if r.get('agent_name')=='failed-'+failure)
 assert record['exit_code']!=0 and record['error']
 if failure!='process':
  assert record['recovery_container'] and 'recover: docker cp ' in p.stderr
  assert not any(line.startswith('rm ') for line in log.read_text().splitlines())
 else: assert 'code 7' in record['error']
print('P1 CLI regressions passed (96 concurrent runs, init, isolation, export recovery)')
PY
