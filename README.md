# Финальный проект 1 семестра

REST API сервис для загрузки и выгрузки данных о ценах. Реализован сложный уровень: поддержка zip/tar архивов, валидация данных, фильтрация по дате и стоимости.

## Используемые технологии

- **Язык:** Go 1.23
- **СУБД:** PostgreSQL 15
- **Веб-фреймворк:** Gorilla Mux
- **Драйвер БД:** lib/pq
- **Контейнеризация:** Docker, Docker Compose
- **Инфраструктура:** Terraform (Yandex Cloud)
- **CI/CD:** GitHub Actions

## Требования к системе

- Docker и Docker Compose (для локального запуска и деплоя)
- Bash 4+
- Для деплоя в Yandex Cloud: [Terraform](https://terraform.io), SSH-ключ `~/.ssh/id_rsa` / `~/.ssh/id_rsa.pub`

## Установка и запуск

### Предварительная подготовка

```bash
# Сборка Docker-образа (обязательно перед запуском)
chmod +x scripts/prepare.sh
./scripts/prepare.sh
```

### Запуск приложения

```bash
chmod +x scripts/run.sh
./scripts/run.sh
```

Скрипт возвращает IP-адрес или `localhost`, по которому доступно приложение.

**Режимы работы `run.sh`:**
- **Локально (по умолчанию):** запуск через `docker compose up` — PostgreSQL на порту 5432, API на 8080
- **CI (GITHUB_ACTIONS=true, без YC):** запуск только контейнера приложения (PostgreSQL уже поднят как сервис)
- **Yandex Cloud (YC_FOLDER_ID + YC_TOKEN):** Terraform создаёт VM, развёртывание через SSH

### Деплой в Yandex Cloud

```bash
export YC_TOKEN="<OAuth-токен>"
export YC_FOLDER_ID="<идентификатор каталога>"
./scripts/run.sh
```

Требуется: Terraform, SSH-ключ `~/.ssh/id_rsa` / `~/.ssh/id_rsa.pub`. VM создаётся через Terraform (`terraform/`), репозиторий копируется по SSH, `docker compose up --build` выполняется на сервере.

### GitHub Actions

Воркфлоу деплоит приложение в Yandex Cloud и запускает тесты на удалённом сервере. Добавьте в Secrets репозитория:

- `YC_TOKEN` — OAuth-токен Yandex Cloud
- `YC_FOLDER_ID` — идентификатор каталога
- `SSH_PRIVATE_KEY` — приватный ключ для доступа к VM (публичный ключ передаётся в Terraform)

## Параметры API

### POST /api/v0/prices

Загрузка данных из архива в базу.

| Параметр | Тип | Описание |
|----------|-----|----------|
| `type` | query | Тип архива: `zip` (по умолчанию) или `tar` |
| `file` | form-data | Файл архива (multipart/form-data) |

Архив должен содержать CSV с колонками: id, create_date (YYYY-MM-DD), name, category, price.

**Ответ (JSON):**
```json
{
  "total_count": 123,
  "duplicates_count": 20,
  "total_items": 100,
  "total_categories": 15,
  "total_price": 100000
}
```

**Пример:**
```bash
curl -X POST -F "file=@data.zip" "http://localhost:8080/api/v0/prices?type=zip"
```

### GET /api/v0/prices

Выгрузка данных в zip-архив с фильтрацией.

| Параметр | Тип | Описание |
|----------|-----|----------|
| `start` | query | Начальная дата (YYYY-MM-DD) |
| `end` | query | Конечная дата (YYYY-MM-DD) |
| `min` | query | Минимальная цена (натуральное число) |
| `max` | query | Максимальная цена (натуральное число) |

**Ответ:** ZIP-архив с файлом `data.csv`.

**Пример:**
```bash
curl -o result.zip "http://localhost:8080/api/v0/prices?start=2024-01-01&end=2024-01-31&min=300&max=1000"
```

## Тестирование

```bash
chmod +x scripts/tests.sh
# Уровень 1 — простой, 2 — продвинутый, 3 — сложный
./scripts/tests.sh 3
```

По умолчанию тесты идут против `localhost:8080`. Для удалённого сервера:

```bash
API_HOST="http://<IP>:8080" DB_HOST="<IP>" ./scripts/tests.sh 3
```

Перед запуском тестов приложение и PostgreSQL должны быть доступны (локально через `./scripts/run.sh` или на удалённом сервере).

Директория `sample_data` — пример разархивированных данных, эквивалентных `sample_data.zip`.

## Структура данных CSV

- `id` — идентификатор продукта (целое число)
- `create_date` — дата создания (YYYY-MM-DD)
- `name` — название продукта
- `category` — категория
- `price` — цена (натуральное число)

## Контакт

К кому можно обращаться в случае вопросов?
