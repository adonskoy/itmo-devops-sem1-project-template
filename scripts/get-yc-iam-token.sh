#!/bin/bash
# Получение и установка IAM-токена Yandex Cloud из авторизованного ключа сервисного аккаунта.
#
# Использование:
#   ./scripts/get-yc-iam-token.sh authorized_key.json
#       — выводит токен в stdout
#
#   eval $(./scripts/get-yc-iam-token.sh authorized_key.json --export)
#       — экспортирует YC_TOKEN и TF_VAR_yc_token в текущий shell
#
#   YC_SERVICE_ACCOUNT_KEY_FILE=key.json eval $(./scripts/get-yc-iam-token.sh --export)
#       — путь к ключу через переменную окружения
set -e

KEY_FILE=""
EXPORT_MODE=""
for arg in "$@"; do
    case "$arg" in
        --export) EXPORT_MODE="--export" ;;
        *)
            if [ -z "$KEY_FILE" ] && [ "$arg" != "-" ]; then
                KEY_FILE="$arg"
            fi
            ;;
    esac
done
KEY_FILE="${KEY_FILE:-$YC_SERVICE_ACCOUNT_KEY_FILE}"

if [ -z "$KEY_FILE" ]; then
    echo "Usage: $0 <path-to-authorized-key.json> [--export]" >&2
    echo "   or: YC_SERVICE_ACCOUNT_KEY_FILE=<path> $0 [--export]" >&2
    echo "" >&2
    echo "Authorized key (authorized_key.json) — JSON-файл ключа сервисного аккаунта," >&2
    echo "созданный в консоли Yandex Cloud (IAM → Сервисные аккаунты → Создать ключ)." >&2
    exit 1
fi

if [ ! -f "$KEY_FILE" ]; then
    echo "Error: Key file not found: $KEY_FILE" >&2
    exit 1
fi

if ! command -v yc &>/dev/null; then
    echo "Error: yc CLI not found. Install: https://cloud.yandex.ru/docs/cli/quickstart" >&2
    exit 1
fi

TOKEN=$(yc --sa-key-file "$KEY_FILE" iam create-token 2>/dev/null) || true
if [ -z "$TOKEN" ]; then
    echo "Error: Failed to get IAM token. Check key file and yc configuration." >&2
    exit 1
fi

if [ "$EXPORT_MODE" = "--export" ]; then
    # printf %q — безопасное экранирование для shell
    printf "export YC_TOKEN=%q\n" "$TOKEN"
    printf "export TF_VAR_yc_token=%q\n" "$TOKEN"
else
    echo "$TOKEN"
fi
