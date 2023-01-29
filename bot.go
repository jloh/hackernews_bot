package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/mattn/go-mastodon"
	"github.com/peterhellberg/hn"

	_ "github.com/go-sql-driver/mysql"
)

func formatToot(item *hn.Item) string {
	var tootText string
	if strings.Contains(item.URL, "https://news.ycombinator.com/item?id=") {
		tootText = fmt.Sprintf("%s\nL: %s", item.Title, item.URL)
	} else {
		tootText = fmt.Sprintf("%s\nL: %s\nC: https://news.ycombinator.com/item?id=%v", item.Title, item.URL, item.ID)
	}

	return tootText
}

func createToot(item *hn.Item, tooter *mastodon.Client) (*mastodon.Status, error) {
	toot, err := tooter.PostStatus(context.Background(), &mastodon.Toot{
		Status: formatToot(item),
	})

	if err != nil {
		return toot, err
	} else {
		return toot, nil
	}
}

// func updateToot(item *hn.Item, toot_id int, tooter *mastodon.Client) error {
// 	_, err := tooter.UpdateStatus(context.Background(), &mastodon.Toot{
// 		Status: formatToot(item),
// 	}, mastodon.ID(fmt.Sprintf("%v", toot_id)))

// 	if err != nil {
// 		return err
// 	} else {
// 		return nil
// 	}
// }

func handleItem(pos int, id int, hn *hn.Client, tooter *mastodon.Client, db *sql.DB) {
	item, err := hn.Item(id)
	if err != nil {
		panic(err)
	}
	var toot_id int
	stmt, err := db.Prepare("SELECT toot_id FROM posts where hn_id=?")
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	// Check if we have any existing IDs
	err = stmt.QueryRow(id).Scan(&toot_id)
	if err != nil {
		switch {
		case err == sql.ErrNoRows:
			log.Printf("Creating toot for: %v\n", item.ID)
			toot, err := createToot(item, tooter)
			if err != nil {
				log.Printf("Failed sending toot: %v\n", err)
			} else {
				log.Printf("Created toot for item %v (%s/@hackernews/%v)", item.ID, os.Getenv("TOOT_SERVER"), toot.ID)
				// Save our toot in the DB
				_, err := db.Exec(`INSERT INTO posts (
					hn_id, toot_id, posted_at, hn_points, hn_comments) VALUES(?,?,?,?,?)`, item.ID, toot.ID, toot.CreatedAt, item.Score, item.Descendants)
				if err != nil {
					log.Printf("Failed saving toot to DB: %v\n", err)
				}
			}
		default:
			log.Printf("Failed getting rows: %s\n", err)
		}
	} else {
		log.Printf("Already tooted %v at pos %v (%s/@hackernews/%v)", id, pos, os.Getenv("TOOT_SERVER"), toot_id)
	}
}

func main() {
	db, err := sql.Open("mysql", os.Getenv("DSN"))
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping: %v", err)
	}

	log.Println("Successfully connected to DB")

	tooter := mastodon.NewClient(&mastodon.Config{
		Server:      os.Getenv("TOOT_SERVER"),
		AccessToken: os.Getenv("TOOT_TOKEN"),
	})

	hn := hn.DefaultClient

	ids, err := hn.TopStories()
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for i, id := range ids[:30] {
		wg.Add(1)
		go func(id int, pos int) {
			defer wg.Done()
			handleItem(pos, id, hn, tooter, db)
		}(id, i+1)
	}

	wg.Wait()
}
