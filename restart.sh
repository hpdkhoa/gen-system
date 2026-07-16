#!/usr/bin/env bash

sudo ./install.sh --no-ollama --no-postgres --skip-model
sudo systemctl restart gen-server
