
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