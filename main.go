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
	TotalCount      int `json:"total_count"`
	DuplicatesCount int `json:"duplicates_count"`
	TotalItems      int `json:"total_items"`
	TotalCategories int `json:"total_categories"`
	TotalPrice      int `json:"total_price"`
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

func initDB(connStr string) error {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS prices (
			id INTEGER NOT NULL,
			create_date DATE NOT NULL,
			name VARCHAR(255) NOT NULL,
			category VARCHAR(255) NOT NULL,
			price INTEGER NOT NULL,
			PRIMARY KEY (id)
		)
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
	seen := make(map[int]bool)
	duplicatesCount := 0

	for _, rec := range records {
		if len(rec) < 5 {
			continue
		}

		id, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		if err != nil || id <= 0 {
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

		if seen[id] {
			duplicatesCount++
			continue
		}
		seen[id] = true

		result = append(result, PriceRecord{
			ID:         id,
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

func postPricesHandler(w http.ResponseWriter, r *http.Request) {
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

	connStr := getDBConnString()
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"db connect: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	dbDuplicates := 0
	for _, rec := range records {
		result, err := db.Exec(
			`INSERT INTO prices (id, create_date, name, category, price) VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (id) DO NOTHING`,
			rec.ID, rec.CreateDate.Format("2006-01-02"), rec.Name, rec.Category, rec.Price,
		)
		if err != nil {
			log.Printf("insert error: %v", err)
			continue
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			dbDuplicates++
		}
	}

	var rows *sql.Rows
	rows, err = db.Query(`SELECT COUNT(*), COUNT(DISTINCT category), COALESCE(SUM(price), 0) FROM prices`)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var totalItems, totalCategories, totalPrice int
	if rows.Next() {
		rows.Scan(&totalItems, &totalCategories, &totalPrice)
	}

	resp := PostResponse{
		TotalCount:      totalCount,
		DuplicatesCount: inputDuplicates + dbDuplicates,
		TotalItems:      totalItems,
		TotalCategories: totalCategories,
		TotalPrice:      totalPrice,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func getPricesHandler(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	minStr := r.URL.Query().Get("min")
	maxStr := r.URL.Query().Get("max")

	if start == "" || end == "" || minStr == "" || maxStr == "" {
		http.Error(w, `{"error":"missing start, end, min or max"}`, http.StatusBadRequest)
		return
	}

	minPrice, err := strconv.Atoi(minStr)
	if err != nil || minPrice <= 0 {
		http.Error(w, `{"error":"min must be positive integer"}`, http.StatusBadRequest)
		return
	}
	maxPrice, err := strconv.Atoi(maxStr)
	if err != nil || maxPrice <= 0 {
		http.Error(w, `{"error":"max must be positive integer"}`, http.StatusBadRequest)
		return
	}

	connStr := getDBConnString()
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT id, create_date, name, category, price FROM prices 
		 WHERE create_date >= $1 AND create_date <= $2 AND price >= $3 AND price <= $4
		 ORDER BY id`,
		start, end, minPrice, maxPrice,
	)
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
			continue
		}
		buf.WriteString(fmt.Sprintf("%d,%s,%s,%s,%d\n", id, createDate, name, category, price))
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

func main() {
	connStr := getDBConnString()
	for i := 0; i < 10; i++ {
		if err := initDB(connStr); err != nil {
			log.Printf("DB init retry %d: %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/v0/prices", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
			case http.MethodPost:
				postPricesHandler(w, r)
			case http.MethodGet:
				getPricesHandler(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}).Methods("POST", "GET")

	port := getEnv("PORT", "8080")
	log.Printf("Listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
