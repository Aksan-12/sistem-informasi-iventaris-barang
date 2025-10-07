// main.go (PERBAIKAN BUG STOK)
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Product struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Price         float64   `json:"price"`
	Stock         int       `json:"stock"`
	ImageFilename string    `json:"image_filename"`
	CreatedAt     time.Time `json:"created_at"`
}

var db *sql.DB
const uploadDir = "./uploads"

func main() {
	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		os.Mkdir(uploadDir, 0755)
	}

	dsn := "root@tcp(127.0.0.1:3306)/go_crud_db?parseTime=true"
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Gagal membuka koneksi DB: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("Koneksi ke database gagal: %v", err)
	}
	fmt.Println("Sukses terhubung ke database MySQL!")

	fs := http.FileServer(http.Dir(uploadDir))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", fs))

	http.HandleFunc("/api/products", productsHandler)
	http.HandleFunc("/api/products/", productHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	fmt.Println("Server berjalan di http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// --- Handlers Utama ---

func productsHandler(w http.ResponseWriter, r *http.Request) {
	setupCORS(&w, r)
	if r.Method == "OPTIONS" { return }

	switch r.Method {
	case "GET":
		getProducts(w, r)
	case "POST":
		createProduct(w, r)
	default:
		http.Error(w, "Metode tidak diizinkan", http.StatusMethodNotAllowed)
	}
}

func productHandler(w http.ResponseWriter, r *http.Request) {
	setupCORS(&w, r)
	if r.Method == "OPTIONS" { return }

	pathParts := strings.Split(r.URL.Path, "/")
	id, err := strconv.Atoi(pathParts[len(pathParts)-1])
	if err != nil {
		http.Error(w, "ID tidak valid", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getProduct(w, r, id)
	case "PUT":
		updateProduct(w, r, id)
	case "DELETE":
		deleteProduct(w, r, id)
	default:
		http.Error(w, "Metode tidak diizinkan", http.StatusMethodNotAllowed)
	}
}

// --- Logika CRUD ---

func getProducts(w http.ResponseWriter, r *http.Request) {
	// PERBAIKAN: Ambil semua kolom termasuk created_at
	rows, err := db.Query("SELECT id, name, description, price, stock, image_filename, created_at FROM products ORDER BY id DESC")
	if err != nil {
		log.Printf("Error query products: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		var imageFilename sql.NullString
		
		// PERBAIKAN: Scan semua field termasuk created_at
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &imageFilename, &p.CreatedAt); err != nil {
			log.Printf("Error scanning product: %v", err)
			continue
		}
		
		p.ImageFilename = imageFilename.String
		products = append(products, p)
	}

	// PERBAIKAN: Log untuk debugging
	log.Printf("Total products fetched: %d", len(products))
	for _, p := range products {
		log.Printf("Product: ID=%d, Name=%s, Stock=%d", p.ID, p.Name, p.Stock)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

func getProduct(w http.ResponseWriter, r *http.Request, id int) {
	var p Product
	var imageFilename sql.NullString
	query := "SELECT id, name, description, price, stock, image_filename, created_at FROM products WHERE id = ?"
	err := db.QueryRow(query, id).Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &imageFilename, &p.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	p.ImageFilename = imageFilename.String
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func createProduct(w http.ResponseWriter, r *http.Request) {
	// PERBAIKAN: Tambahkan logging
	log.Println("=== CREATE PRODUCT REQUEST ===")
	
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Printf("Error parsing form: %v", err)
		http.Error(w, "File terlalu besar (maks 10MB)", http.StatusBadRequest)
		return
	}
	
	// PERBAIKAN: Log semua form values
	log.Printf("Form values: name=%s, description=%s, price=%s, stock=%s", 
		r.FormValue("name"), r.FormValue("description"), r.FormValue("price"), r.FormValue("stock"))
	
	price, err := strconv.ParseFloat(r.FormValue("price"), 64)
	if err != nil {
		log.Printf("Error parsing price: %v", err)
		http.Error(w, "Harga tidak valid", http.StatusBadRequest)
		return
	}
	
	stock, err := strconv.Atoi(r.FormValue("stock"))
	if err != nil {
		log.Printf("Error parsing stock: %v", err)
		http.Error(w, "Jumlah stok tidak valid", http.StatusBadRequest)
		return
	}
	
	imageFilename, err := saveUploadedFile(r, "image")
	if err != nil {
		log.Printf("Error saving file: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	p := Product{
		Name:          r.FormValue("name"),
		Description:   r.FormValue("description"),
		Price:         price,
		Stock:         stock,
		ImageFilename: imageFilename,
	}
	
	log.Printf("Inserting product: %+v", p)
	
	query := "INSERT INTO products(name, description, price, stock, image_filename) VALUES(?, ?, ?, ?, ?)"
	res, err := db.Exec(query, p.Name, p.Description, p.Price, p.Stock, p.ImageFilename)
	if err != nil {
		log.Printf("Error inserting product: %v", err)
		if p.ImageFilename != "" {
			os.Remove(filepath.Join(uploadDir, p.ImageFilename))
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	id, _ := res.LastInsertId()
	p.ID = int(id)
	
	log.Printf("Product created successfully with ID: %d", p.ID)
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func updateProduct(w http.ResponseWriter, r *http.Request, id int) {
	// PERBAIKAN: Tambahkan logging
	log.Printf("=== UPDATE PRODUCT REQUEST for ID: %d ===", id)
	
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Printf("Error parsing form: %v", err)
		http.Error(w, "File terlalu besar (maks 10MB)", http.StatusBadRequest)
		return
	}
	
	// PERBAIKAN: Log semua form values
	log.Printf("Form values: name=%s, description=%s, price=%s, stock=%s", 
		r.FormValue("name"), r.FormValue("description"), r.FormValue("price"), r.FormValue("stock"))
	
	var oldImageFilename string
	db.QueryRow("SELECT image_filename FROM products WHERE id = ?", id).Scan(&oldImageFilename)
	
	newImageFilename, err := saveUploadedFile(r, "image")
	if err != nil {
		log.Printf("Error saving file: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	price, err := strconv.ParseFloat(r.FormValue("price"), 64)
	if err != nil {
		log.Printf("Error parsing price: %v", err)
		http.Error(w, "Harga tidak valid", http.StatusBadRequest)
		return
	}
	
	stock, err := strconv.Atoi(r.FormValue("stock"))
	if err != nil {
		log.Printf("Error parsing stock: %v", err)
		http.Error(w, "Jumlah stok tidak valid", http.StatusBadRequest)
		return
	}
	
	p := Product{
		ID:          id,
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Price:       price,
		Stock:       stock,
	}
	
	query := "UPDATE products SET name=?, description=?, price=?, stock=?"
	args := []interface{}{p.Name, p.Description, p.Price, p.Stock}

	if newImageFilename != "" {
		p.ImageFilename = newImageFilename
		query += ", image_filename=?"
		args = append(args, p.ImageFilename)
	}

	query += " WHERE id=?"
	args = append(args, p.ID)

	log.Printf("Executing update query: %s with args: %+v", query, args)
	
	result, err := db.Exec(query, args...)
	if err != nil {
		log.Printf("Error updating product: %v", err)
		if newImageFilename != "" {
			os.Remove(filepath.Join(uploadDir, newImageFilename))
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("Rows affected: %d", rowsAffected)

	if newImageFilename != "" && oldImageFilename != "" {
		os.Remove(filepath.Join(uploadDir, oldImageFilename))
	}
	
	log.Printf("Product updated successfully: %+v", p)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func deleteProduct(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("=== DELETE PRODUCT REQUEST for ID: %d ===", id)
	
	var imageFilename sql.NullString
	db.QueryRow("SELECT image_filename FROM products WHERE id = ?", id).Scan(&imageFilename)
	
	_, err := db.Exec("DELETE FROM products WHERE id = ?", id)
	if err != nil {
		log.Printf("Error deleting product: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	if imageFilename.Valid && imageFilename.String != "" {
		os.Remove(filepath.Join(uploadDir, imageFilename.String))
	}
	
	log.Printf("Product deleted successfully with ID: %d", id)
	w.WriteHeader(http.StatusNoContent)
}

// --- Fungsi Helper ---

func saveUploadedFile(r *http.Request, formFileName string) (string, error) {
	file, handler, err := r.FormFile(formFileName)
	if err != nil {
		if err == http.ErrMissingFile {
			log.Println("No file uploaded")
			return "", nil 
		}
		return "", err
	}
	defer file.Close()
	
	ext := filepath.Ext(handler.Filename)
	newFileName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	
	log.Printf("Saving file: %s", newFileName)
	
	dst, err := os.Create(filepath.Join(uploadDir, newFileName))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	
	_, err = io.Copy(dst, file)
	return newFileName, err
}

func setupCORS(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Authorization")
}