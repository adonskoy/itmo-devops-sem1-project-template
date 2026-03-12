package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

const (
	dbHost     = "DB_HOST"
	dbPort     = "DB_PORT"
	dbUser     = "DB_USER"
	dbPassword = "DB_PASSWORD"
	dbName     = "DB_NAME"
)

type PriceRecord struct {
	ID         int       `json:"id"`
	CreateDate time.Time `json:"create_date"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Price      int      `json:"price"`
}

type PostResponse struct {
	TotalCount      int     `json:"total_count"`
	DuplicatesCount int     `json:"duplicates_count"`
	TotalItems      int     `json:"total_items"`
	TotalCategories int     `json:"total_categories"`
	TotalPrice      float64 `json:"total_price"`
}

func getDBConnString() string {
	host := getEnv(dbHost, "localhost")
	port := getEnv(dbPort, "5432")
	user := getEnv(dbUser, "validator")
	password := getEnv(dbPassword, "val1dat0r")
	db := getEnv(dbName, "project-sem-1")
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, db)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS prices (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            category VARCHAR(255) NOT NULL,
            price DECIMAL(10,2) NOT NULL,
            create_date TIMESTAMP NOT NULL
        )
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_prices_unique
		ON prices (name, category, price, create_date)
	`)
	return err
}

func processCSV(content []byte) ([]PriceRecord, int, error) {
	r := csv.NewReader(bytes.NewReader(content))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, 0, err
	}

	var result []PriceRecord
	seen := make(map[string]bool)
	duplicatesCount := 0

	for _, rec := range records {
		if len(rec) < 5 {
			continue
		}

		_, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		if err != nil {
			continue
		}

		var createDate time.Time
		var name, category string
		var price int
		var dateFound, priceFound bool

		for j := 1; j < len(rec); j++ {
			s := strings.TrimSpace(rec[j])
			if t, err := time.Parse("2006-01-02", s); err == nil && len(s) == 10 {
				createDate = t
				dateFound = true
			} else if p, err := strconv.Atoi(s); err == nil && p > 0 {
				price = p
				priceFound = true
			} else if s != "" {
				if name == "" {
					name = s
				} else {
					category = s
				}
			}
		}

		if !dateFound || !priceFound || name == "" || category == "" {
			continue
		}

		key := fmt.Sprintf("%s|%s|%d|%s", name, category, price, createDate.Format("2006-01-02"))
		if seen[key] {
			duplicatesCount++
			continue
		}
		seen[key] = true

		result = append(result, PriceRecord{
			ID:         0,
			CreateDate: createDate,
			Name:       name,
			Category:   category,
			Price:      price,
		})
	}

	return result, duplicatesCount, nil
}

func extractCSVFromZip(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("no csv file in archive")
}

func extractCSVFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(strings.ToLower(h.Name), ".csv") {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("no csv file in archive")
}

func postPricesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		archiveType := r.URL.Query().Get("type")
		if archiveType == "" {
			archiveType = "zip"
		}
		archiveType = strings.ToLower(archiveType)
		if archiveType != "zip" && archiveType != "tar" {
			http.Error(w, `{"error":"type must be zip or tar"}`, http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"missing file in form"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		var csvContent []byte
		if archiveType == "zip" {
			csvContent, err = extractCSVFromZip(file)
		} else {
			body, _ := io.ReadAll(file)
			csvContent, err = extractCSVFromTar(bytes.NewReader(body))
		}
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		records, inputDuplicates, err := processCSV(csvContent)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		all, _ := csv.NewReader(bytes.NewReader(csvContent)).ReadAll()
		totalCount := 0
		for _, rec := range all {
			if len(rec) >= 5 {
				totalCount++
			}
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		insertedCount := 0
		dbDuplicates := 0
		for _, rec := range records {
			result, err := tx.Exec(
				`INSERT INTO prices (create_date, name, category, price) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (name, category, price, create_date) DO NOTHING`,
				rec.CreateDate.Format("2006-01-02"), rec.Name, rec.Category, rec.Price,
			)
			if err != nil {
				log.Printf("insert error: %v", err)
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
			affected, err := result.RowsAffected()
			if err != nil {
				log.Printf("RowsAffected error: %v", err)
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
			if affected > 0 {
				insertedCount++
			} else {
				dbDuplicates++
			}
		}

		rows, err := tx.Query(`SELECT COUNT(DISTINCT category), COALESCE(SUM(price), 0) FROM prices`)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var totalCategories int
		var totalPrice float64
		if rows.Next() {
			if err := rows.Scan(&totalCategories, &totalPrice); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if err := rows.Err(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		resp := PostResponse{
			TotalCount:      totalCount,
			DuplicatesCount: inputDuplicates + dbDuplicates,
			TotalItems:      insertedCount,
			TotalCategories: totalCategories,
			TotalPrice:      totalPrice,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode response: %v", err)
		}
	}
}

func getPricesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")
		minStr := r.URL.Query().Get("min")
		maxStr := r.URL.Query().Get("max")

		var conditions []string
		var args []interface{}
		argNum := 1

		if start != "" {
			conditions = append(conditions, fmt.Sprintf("create_date >= $%d", argNum))
			args = append(args, start)
			argNum++
		}
		if end != "" {
			conditions = append(conditions, fmt.Sprintf("create_date <= $%d", argNum))
			args = append(args, end)
			argNum++
		}
		if minStr != "" {
			minPrice, err := strconv.Atoi(minStr)
			if err != nil || minPrice < 0 {
				http.Error(w, `{"error":"min must be non-negative integer"}`, http.StatusBadRequest)
				return
			}
			conditions = append(conditions, fmt.Sprintf("price >= $%d", argNum))
			args = append(args, minPrice)
			argNum++
		}
		if maxStr != "" {
			maxPrice, err := strconv.Atoi(maxStr)
			if err != nil || maxPrice < 0 {
				http.Error(w, `{"error":"max must be non-negative integer"}`, http.StatusBadRequest)
				return
			}
			conditions = append(conditions, fmt.Sprintf("price <= $%d", argNum))
			args = append(args, maxPrice)
			argNum++
		}

		query := `SELECT id, create_date, name, category, price FROM prices`
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		query += " ORDER BY id"

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var buf bytes.Buffer
		buf.WriteString("id,create_date,name,category,price\n")
		for rows.Next() {
			var id, price int
			var createDate string
			var name, category string
			if err := rows.Scan(&id, &createDate, &name, &category, &price); err != nil {
				log.Printf("Scan error: %v", err)
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
			buf.WriteString(fmt.Sprintf("%d,%s,%s,%s,%d\n", id, createDate, name, category, price))
		}
		if err := rows.Err(); err != nil {
			log.Printf("rows.Err: %v", err)
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		zipBuf := new(bytes.Buffer)
		zw := zip.NewWriter(zipBuf)
		zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(w, flate.DefaultCompression)
		})
		wf, err := zw.Create("data.csv")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wf.Write(buf.Bytes())
		zw.Close()

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=data.zip")
		w.Write(zipBuf.Bytes())
	}
}

func main() {
	connStr := getDBConnString()
	var db *sql.DB
	var err error
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", connStr)
		if err != nil {
			log.Printf("DB open retry %d: %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}
		if err := initDB(db); err != nil {
			log.Printf("DB init retry %d: %v", i+1, err)
			db.Close()
			db = nil
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}
	if db == nil {
		log.Fatal("failed to connect to database")
	}
	defer db.Close()

	r := mux.NewRouter()
	r.HandleFunc("/api/v0/prices", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			postPricesHandler(db).ServeHTTP(w, r)
		case http.MethodGet:
			getPricesHandler(db).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}).Methods("POST", "GET")

	port := getEnv("PORT", "8080")
	log.Printf("Listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
