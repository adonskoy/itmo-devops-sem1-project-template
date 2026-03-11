#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
IMAGE_NAME="project-sem-1"
TERRAFORM_DIR="$PROJECT_ROOT/terraform"

cd "$PROJECT_ROOT"

# Переменные для подключения к БД
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-validator}"
DB_PASSWORD="${DB_PASSWORD:-val1dat0r}"
DB_NAME="${DB_NAME:-project-sem-1}"

# GitHub Actions или другая CI: Postgres уже запущен как service (только если нет деплоя в облако)
if { [ -n "${CI}" ] || [ -n "${GITHUB_ACTIONS}" ]; } && [ -z "${YC_TOKEN}" ] && [ -z "${YC_FOLDER_ID}" ]; then
    echo "CI mode: starting app container (Postgres already running)..."

    docker rm -f project-sem-1-app 2>/dev/null || true

    if [ "$(uname)" = "Linux" ]; then
        docker run -d --name project-sem-1-app --network host \
            -e DB_HOST=127.0.0.1 \
            -e DB_PORT="$DB_PORT" \
            -e DB_USER="$DB_USER" \
            -e DB_PASSWORD="$DB_PASSWORD" \
            -e DB_NAME="$DB_NAME" \
            "$IMAGE_NAME"
    else
        docker run -d --name project-sem-1-app -p 8080:8080 \
            -e DB_HOST=host.docker.internal \
            -e DB_PORT="$DB_PORT" \
            -e DB_USER="$DB_USER" \
            -e DB_PASSWORD="$DB_PASSWORD" \
            -e DB_NAME="$DB_NAME" \
            "$IMAGE_NAME"
    fi

    echo "Waiting for app to start..."
    sleep 5
    echo "Application started. IP: localhost"
    exit 0
fi

# Yandex Cloud: Terraform + деплой через SSH
if [ -n "${YC_TOKEN}" ] || [ -n "${YC_FOLDER_ID}" ]; then
    echo "Deploying to Yandex Cloud via Terraform..."

    if ! command -v terraform &>/dev/null; then
        echo "Error: terraform not found. Install from https://terraform.io"
        exit 1
    fi

    if [ ! -f ~/.ssh/id_rsa.pub ]; then
        echo "Error: ~/.ssh/id_rsa.pub not found. SSH key is required."
        exit 1
    fi

    SSH_USER="ubuntu"
    REMOTE_DIR="/home/${SSH_USER}/project-sem-1"

    # Terraform variables
    export TF_VAR_yc_token="${YC_TOKEN}"
    export TF_VAR_yc_folder_id="${YC_FOLDER_ID}"
    export TF_VAR_ssh_public_key_path="$HOME/.ssh/id_rsa.pub"

    if [ -z "$TF_VAR_yc_folder_id" ]; then
        echo "Error: YC_FOLDER_ID is required"
        exit 1
    fi

    if [ -z "$TF_VAR_yc_token" ]; then
        echo "Error: YC_TOKEN is required"
        exit 1
    fi

    cd "$TERRAFORM_DIR"
    terraform init -input=false
    terraform apply -auto-approve -input=false

    VM_IP=$(terraform output -raw vm_ip)
    cd "$PROJECT_ROOT"

    echo "VM IP: $VM_IP. Waiting for SSH..."
    for i in $(seq 1 60); do
        if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new \
            -o UserKnownHostsFile=/dev/null -i ~/.ssh/id_rsa "${SSH_USER}@${VM_IP}" "echo ok" 2>/dev/null; then
            break
        fi
        if [ "$i" -eq 60 ]; then
            echo "Error: SSH connection timeout"
            exit 1
        fi
        sleep 5
    done

    echo "Deploying via SSH..."
    TARBALL="/tmp/project-sem-1-$(date +%s).tar.gz"
    tar --exclude='.git' -czf "$TARBALL" -C "$PROJECT_ROOT" .

    scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ~/.ssh/id_rsa \
        "$TARBALL" \
        "${SSH_USER}@${VM_IP}:/tmp/project-sem-1.tar.gz"

    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ~/.ssh/id_rsa "${SSH_USER}@${VM_IP}" bash -s << REMOTEEOF
        set -e
        export DEBIAN_FRONTEND=noninteractive
        if ! command -v docker &>/dev/null; then
            echo "Installing Docker..."
            curl -fsSL https://get.docker.com | sh
            sudo usermod -aG docker \$USER 2>/dev/null || true
        fi
        mkdir -p $REMOTE_DIR
        tar -xzf /tmp/project-sem-1.tar.gz -C $REMOTE_DIR
        rm -f /tmp/project-sem-1.tar.gz
        cd $REMOTE_DIR && sudo docker compose up -d --build
        echo "Deployment complete."
REMOTEEOF

    rm -f "$TARBALL"
    echo ""
    echo "Deployment complete. Application available at http://${VM_IP}:8080"
    echo "$VM_IP"
    exit 0
fi

# Локальный режим
echo "Starting with Docker Compose..."
docker compose up -d

echo "Waiting for services..."
sleep 10

echo "Application ready at http://localhost:8080"
echo "localhost"
