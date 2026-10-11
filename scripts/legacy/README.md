# Legacy scripts

I keep these for the record. None of them is part of the documented install or benchmark path.

| Script | What it was | Use instead |
|---|---|---|
| `setup.sh` | early all in one setup (Node 18) | `install.sh` |
| `quant_sweep.sh` | the first quantization sweep, compile pass as its quality axis | `bench/run-quant-sweep.sh` (stub rate, see `bench/README.md`) |
| `verify-v23.1.sh` | one shot capability check for v23.1 | `make test`, then `bench/run-all.sh` |
| `gen-verify.sh` | v23.26 end to end check: build, benchmark, generate an app, curl it, continue it | `make test`, `bench/run-all.sh`, and `bench/verify-postgres.sh` for the live database path |
| `refresh.sh` | rebuild and restart in one command | `build.sh` then `sudo systemctl restart gen-server` |
| `restart.sh` | reinstall without Ollama, Postgres or the model pull | `install.sh --no-ollama --no-postgres --skip-model` |
| `run-local.sh` | start `understand-server` on a hardcoded `/var/lib/gen-system` root | `understand-server --root <your repo>` |
