# tusker
- Tusker is a full-text and semantic search engine for exploring multilingual news headlines and articles. The project uses Go 1.25 and focuses on importing, processing, and storing news data from various sources (currently Kaggle datasets).
- The application provides a REST API for searching and retrieving news articles based on keywords, dates, and languages. It leverages PostgreSQL and Elasticsearch for data storage and supports full-text and semantic search capabilities.
## Commands
### Migrations
- Install golang-migrate
```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
- Run migrations
- - Example
```bash
migrate -path db/migrations -database "postgres://username:password@localhost:5432/news_db?sslmode=disable" up
```
- Local Postgres with Docker
```bash
migrate -path db/migrations -database "postgres://news_user:news_password@localhost:54320/news_db?sslmode=disable" up
```
### OpenAPI Schema gen
- Install swag CLI
```bash
go install github.com/swaggo/swag/cmd/swag@latest
```
- Generate OpenAPI spec
```bash
swag init -g cmd/news_api/main.go -o ./api/openapi-spec
```
### Run formatter
```bash
go fmt ./...
```

### Run static analysis
```bash
go vet ./...
```

### Run tests
```bash
go test ./...
```
### Run linter
- Install golangci-lint
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```
```bash
golangci-lint run
```
