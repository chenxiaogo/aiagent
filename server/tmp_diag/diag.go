package main

import (
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type A struct{ ID int64; Name, Category, Status string }
func (A) TableName() string { return "agents" }

func main() {
	dsn := "host=127.0.0.1 user=postgres password=postgres dbname=aiagent port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil { fmt.Println("ERR", err); return }
	var rows []A
	db.Raw("SELECT id, name, category, status FROM agents ORDER BY id").Scan(&rows)
	for _, r := range rows { fmt.Printf("id=%d name=%q category=%q status=%q\n", r.ID, r.Name, r.Category, r.Status) }
}
