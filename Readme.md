
```
microvm_script_runner/
├── api/                  # REST API handlers
│   ├── handlers.go       # HTTP handlers for endpoints
│   └── routes.go         # Route definitions
├── jobs/                 # Job definitions and Redis queue integration
│   └── processor.go
├── runner/               # Firecracker-based script runner
│   └── firecracker.go
├── storage/              # Script and log storage (local or S3)
│   └── local.go
├── db/                   # SQLite DB for job metadata
│   └── models.go
├── config/               # Configuration and constants
│   └── config.go
├── vm/                   # Minimal VM image tools or helper scripts
│   └── build_rootfs.sh
├── main.go               # Entry point (API and queue runner)
├── go.mod
└── README.md

```

## RUN

run `build_rootfs.sh` to build the linux vm

```
sudo chmod +x build_rootfs.sh

sudo ./build_rootfs.sh
```


run `sudo go run .`

then create a script e.g `test_script.sh` to upload and run

```
#!/bin/sh
echo "Hello from MicroVM!"
echo "Current date: $(date)"
echo "System info:"
uname -a
echo "Available memory:"
free -m
echo "Process list:"
ps aux

```

then run the following api

```
$ curl -X POST -F "script=@test_script.sh" http://localhost:8080/scripts
{"script_id":"45998174-ffaf-4c44-be62-35b931b3e916"}

$ curl -X POST http://localhost:8080/scripts/45998174-ffaf-4c44-be62-35b931b3e916/run
{"job_id":"f7d8784d-0e22-4a3c-8bc5-a2f3b52d2b5c"}

$ curl http://localhost:8080/jobs/f7d8784d-0e22-4a3c-8bc5-a2f3b52d2b5c
{"ID":"f7d8784d-0e22-4a3c-8bc5-a2f3b52d2b5c","ScriptID":"45998174-ffaf-4c44-be62-35b931b3e916","Status":"running","LogPath":"logs/f7d8784d-0e22-4a3c-8bc5-a2f3b52d2b5c.log","StartedAt":"2025-06-12T22:39:10+01:00","FinishedAt":""}

```

then check the stdout for the script output

```
===== SCRIPT EXECUTION START =====
Hello from MicroVM!
Current date: Thu Jun 12 21:50:09 UTC 2025
System info:
Linux (none) 4.14.174 #2 SMP Wed Jul 14 11:47:24 UTC 2021 x86_64 GNU/Linux
Available memory:
              total        used        free      shared  buff/cache   available
Mem:            112           6         104           0           2         103
Swap:             0           0           0
Process list:
PID   USER     TIME  COMMAND
    1 0         0:00 {init} /bin/sh /init
    2 0         0:00 [kthreadd]
    3 0         0:00 [kworker/0:0]
    4 0         0:00 [kworker/0:0H]
    5 0         0:00 [kworker/u2:0]
    6 0         0:00 [mm_percpu_wq]
    7 0         0:00 [ksoftirqd/0]
    8 0         0:00 [rcu_sched]
    9 0         0:00 [rcu_bh]
   10 0         0:00 [migration/0]
   11 0         0:00 [cpuhp/0]
   12 0         0:00 [kdevtmpfs]
   13 0         0:00 [netns]
   14 0         0:00 [kworker/u2:1]
   23 0         0:00 [kworker/0:1]
  147 0         0:00 [oom_reaper]
  148 0         0:00 [writeback]
  150 0         0:00 [kcompactd0]
  151 0         0:00 [ksmd]
  152 0         0:00 [crypto]
  153 0         0:00 [kintegrityd]
  155 0         0:00 [kblockd]
  268 0         0:00 [kauditd]
  273 0         0:00 [kswapd0]
  350 0         0:00 [kworker/u3:0]
  406 0         0:00 [kthrotld]
  440 0         0:00 [kworker/0:1H]
  444 0         0:00 [iscsi_eh]
  473 0         0:00 [ipv6_addrconf]
  474 0         0:00 [kworker/0:2]
  483 0         0:00 [kstrp]
  501 0         0:00 [jbd2/vda-8]
  502 0         0:00 [ext4-rsv-conver]
  515 0         0:00 [jbd2/vdb-8]
  516 0         0:00 [ext4-rsv-conver]
  520 0         0:00 {9c8c62c0-0bff-4} /bin/sh /mnt/script/9c8c62c0-0bff-48e1-8
  524 0         0:00 ps aux
===== SCRIPT EXECUTION END (EXIT CODE: 0) =====
Powering off VM...
[    1.362435] reboot: System halted
```
