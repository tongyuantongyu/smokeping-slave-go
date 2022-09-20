import os
import subprocess
import glob
import datetime

ver = subprocess.run("git describe --tags", shell=True, capture_output=True)
ver = ver.stdout.decode().strip()

full_ver = subprocess.run("git describe --always --dirty --tags", shell=True, capture_output=True)
full_ver = full_ver.stdout.decode().strip()

cur = datetime.datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S')

t = [['linux', 'amd64'],
     ['linux', 'arm'],
     ['linux', 'arm64'],
     ['linux', 'mips'],
     ['linux', 'mipsle'],
     ['windows', 'amd64'],
]

for o, a in t:
    subprocess.run(
        f'''SET GOOS={o}&&SET GOARCH={a}&&SET GOMIPS=softfloat&&'''
        f'''go build -tags="jsoniter" -ldflags="-s -w '''
        f'''-X main.fullVersion={full_ver} -X \'main.buildDate={cur}\'" '''
        f'''-o smokeping-worker_{o}_{a}{".exe" if o == "windows" else ""} '''
        f'''../cmd/smokeping''', shell=True
    )

subprocess.run(
    f'''SET GOOS={o}&&SET GOARCH={a}&&SET GOMIPS=softfloat&&'''
    f'''go build -tags="jsoniter" -ldflags="-s -w -H windowsgui '''
    f'''-X main.fullVersion={full_ver} -X \'main.buildDate={cur}\'" '''
    f'''-o smokeping-worker_windowsgui_amd64.exe '''
    f'''../cmd/smokeping''', shell=True
)
