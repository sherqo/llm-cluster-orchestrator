# LLM Cluster Orchestrator

## Project Structure

```
llm-cluster-orchestrator/
├── README.md                          # Main project README
├── Makefile                           # Build and utility commands
├── docker-compose.yml                 # Docker services configuration
├── CODEBASE_ANALYSIS.md               # Codebase analysis documentation
│
├── agent/                             # Agent component (local resource manager)
│   └── README.md                      # Agent documentation
│
├── master/                            # Master component (cluster orchestrator - Go)
│   ├── main.go                        # Master application entry point
│   ├── go.mod                         # Go module dependencies
│   ├── go.sum                         # Go dependencies lock file
│   ├── tools.go                       # Utility tools
│   ├── README.md                      # Master documentation
│   │
│   ├── lib/                           # Core libraries
│   │   ├── server.go                  # Server implementation
│   │   ├── router.go                  # Request routing logic
│   │   ├── strategy.go                # Scheduling strategies
│   │   ├── worker.go                  # Worker management
│   │   ├── inflight.go                # In-flight request tracking
│   │   └── verbose.go                 # Logging utilities
│   │
│   ├── generated/                     # Generated gRPC code
│   │   ├── agent.pb.go                # Agent protocol buffers
│   │   ├── worker.pb.go               # Worker protocol buffers
│   │   └── worker_grpc.pb.go          # Worker gRPC service
│   │
│   └── db/                            # Database files
│       ├── requests.db                # Request tracking database
│       ├── log.db                     # Activity log database
│       └── migrations/                # Database migration scripts
│
├── worker/                            # Worker component (task processor - Python)
│   ├── main.py                        # Worker application entry point
│   ├── requirements.txt               # Python dependencies
│   ├── README.md                      # Worker documentation
│   ├── worker_pb2.py                  # Generated protocol buffers
│   └── worker_pb2_grpc.py             # Generated gRPC service
│
├── proto/                             # Protocol Buffer definitions
│   ├── agent.proto                    # Master-Agent communication protocol
│   ├── worker.proto                   # Master-Worker communication protocol
│   └── README.md                      # Proto documentation
│
├── vector-db/                         # Vector database service
│   ├── Dockerfile                     # Vector DB Docker image
│   ├── docker-compose.yaml            # Vector DB composition
│   ├── requirements.txt               # Python dependencies
│   ├── entrypoint.sh                  # Container startup script
│   ├── README.md                      # Vector DB documentation
│   │
│   ├── scripts/                       # Database operations
│   │   ├── init_db.py                 # Database initialization
│   │   └── query.py                   # Database query utilities
│   │
│   └── embedded-docs/                 # Sample embedded documents (WWII history)
│       ├── 01-causes-of-wwii.md
│       ├── 02-pearl-harbor.md
│       ├── 03-d-day.md
│       ├── 04-battle-of-stalingrad.md
│       ├── 05-holocaust.md
│       ├── 06-battle-of-britain.md
│       ├── 07-atomic-bombs.md
│       ├── 08-end-of-wwii.md
│       ├── 09-major-leaders.md
│       ├── 10-axis-powers.md
│       ├── 11-allied-powers.md
│       ├── 12-blitzkrieg.md
│       ├── 13-north-africa-campaign.md
│       ├── 14-pacific-theater.md
│       └── 15-european-theater.md
│
└── docs/                              # Documentation
    ├── system.excalidraw              # Architecture diagram (Excalidraw format)
    └── arch.md                        # Architecture documentation
```

## Architecture

You will find the architectural design of the project in the `docs/system.excalidraw` file.

to run it, you will need to have `excalidraw` installed on your machine.

---

some may ask, what will happen if the master goes down? well, idk, it should be restarte. same thing applied on the agents too

the restart maybe get done by the kernel or using a guy like `systemd` in linux
