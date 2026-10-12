import fcntl, os, subprocess, sys, time
lock_path='/tmp/library-performance-audit-20261012.lock'
with open(lock_path,'w') as lock:
    print('WAITING_FOR_AUDIT_LOCK', ' '.join(sys.argv[1:]), flush=True)
    fcntl.flock(lock,fcntl.LOCK_EX)
    print('AUDIT_LOCK_ACQUIRED',time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),flush=True)
    env=os.environ.copy()
    env.setdefault('GOCACHE','/tmp/library-performance-gocache-20261012')
    env.setdefault('GOWORK','off')
    result=subprocess.run(sys.argv[1:],env=env)
    print('AUDIT_LOCK_RELEASED', result.returncode,flush=True)
    sys.exit(result.returncode)
