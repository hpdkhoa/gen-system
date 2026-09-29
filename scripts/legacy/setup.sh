#!/usr/bin/env bash

# echo "🚀 Starting full local setup..."

# ----------------------------

# 🧰 Auto-install dependencies

# ----------------------------

install_go() {
# echo "📦 Installing Go..."
sudo apt update
sudo apt install -y golang
}

install_node() {
# echo "📦 Installing Node.js..."
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt install -y nodejs
}

install_docker() {
# echo "📦 Installing Docker..."
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
}

install_curl() {
# echo "📦 Installing curl..."
sudo apt update
sudo apt install -y curl
}

check_or_install () {
if ! command -v $1 &> /dev/null
then
# echo "⚠️ $1 not found, installing..."
$2
else
# echo "✅ $1 already installed"
fi
}

check_npm () {
if ! command -v npm &> /dev/null
then
# echo "❌ npm missing even though node exists."
# echo "👉 Please reinstall Node.js manually."
exit 1
else
# echo "✅ npm already installed"
fi
}

# Execute checks

check_or_install go install_go
check_or_install node install_node
check_npm
check_or_install docker install_docker
check_or_install curl install_curl

# echo "✅ All core dependencies are ready"


# ----------------------------

# 🤖 Install Ollama (if missing)

# ----------------------------

if ! command -v ollama &> /dev/null
then
# echo "📦 Installing Ollama..."
curl -fsSL https://ollama.com/install.sh | sh
else
# echo "✅ Ollama already installed"
fi

# ----------------------------

# 🤖 Pull models

# ----------------------------

# echo "⬇️ Pulling AI models..."

ollama pull codellama:7b || true
ollama pull deepseek-coder:6.7b || true
ollama pull mistral:7b || true

# echo "✅ Models ready"

# ----------------------------

# Docker start

# ----------------------------

# echo "Starting DB..."

docker compose up -d

# echo "Done"

# ----------------------------

# 🔧 Backend setup

# ----------------------------

# echo "🔧 Setting up backend..."

cd backend
go mod tidy
cd ..

# echo "✅ Backend ready"

# ----------------------------

# 🌐 Frontend setup

# ----------------------------

# echo "🌐 Setting up web frontend..."

cd frontend
npm install
cd ..

# echo "✅ Web ready"

# ----------------------------

# 📱 Mobile setup

# ----------------------------

# echo "📱 Setting up mobile..."

cd mobile
npm install
npm install @vitejs/plugin-react@4
cd ..

# echo "✅ Mobile ready"

# ----------------------------

# ⚡ Start services

# ----------------------------

# echo "🚀 Starting services..."

# Backend

(cd backend && go run cmd/main.go) &
BACKEND_PID=$!

# Frontend web

(cd frontend && npm run dev) &
WEB_PID=$!

# Expo (mobile)

(cd mobile && npx expo start) &
MOBILE_PID=$!

# echo ""
# echo "🎉 SYSTEM READY"
# echo "----------------------------"
# echo "Backend:  http://localhost:28080"
# echo "Web:      http://localhost:3000"
# echo "Mobile:   Expo DevTools"
# echo ""
# echo "Generator:"
# echo "cd generator && go run main.go generate --name Todo"
# echo ""
# echo "Press CTRL+C to stop all services"

wait

