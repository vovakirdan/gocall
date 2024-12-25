package database
//TODO do it with GORM
import (
	"os"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"log"
)

func InitDatabase() {
	logger := log.New(os.Stdout, "database: ", log.LstdFlags)
	// if already inited return false
	if os.Getenv("INIT_DATABASE") == "true" {
		logger.Println("Database already inited")
		return 
	}
	defer func() {
		logger.Println("Database inited")
		os.Setenv("INIT_DATABASE", "true")
	}()
	// create database if not exists in ../data/database.db
	newPath := filepath.Join(".", "data")
	os.MkdirAll(newPath, os.ModePerm)
	_, err := os.Create("./data/database.db")
	if err != nil {
		logger.Fatalf("Error creating database.db: %s", err)
		return
	}

	// read create_tables.sql and execute it
	db, err := sql.Open("sqlite3", "./data/database.db")
	if err != nil {
		logger.Fatalf("Error opening database.db: %s", err)
		return
	}
	defer db.Close()

	sql, err := os.ReadFile("./database/create_tables.sql")
	if err != nil {
		logger.Fatalf("Error reading create_tables.sql: %s", err)
		return
	}

	_, err = db.Exec(string(sql))
	if err != nil {
		logger.Fatalf("Error executing create_tables.sql: %s", err)
		return
	}
}

func GetDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "../data/database.db")
	if err != nil {
		return nil, err
	}
	return db, nil
}

func CloseDatabase(db *sql.DB) {
	db.Close()
}

func RegisterRoom(db *sql.DB, roomId, owner string) error {
	_, err := db.Exec("INSERT INTO rooms (name, owner) VALUES (?, ?)", roomId)
	return err
}

func GetRoomOwner(db *sql.DB, roomId string) (string, error) {
	var owner string
	err := db.QueryRow("SELECT owner FROM rooms WHERE name = ?", roomId).Scan(&owner)
	return owner, err
}
