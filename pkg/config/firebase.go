package config

import (
	"context"
	"log"
	"os"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

var FirebaseApp *firebase.App

func InitFirebase() {
	ctx := context.Background()

	credPath := os.Getenv("FIREBASE_CREDENTIALS_PATH")
	if credPath == "" {
		credPath = "pkg/config/leti-firebase.json"
	}

	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		log.Fatal("FIREBASE_PROJECT_ID environment variable is required")
	}

	opt := option.WithAuthCredentialsFile(option.ServiceAccount, credPath)

	app, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: projectID,
	}, opt)
	if err != nil {
		log.Fatalf("error initializing firebase: %v", err)
	}

	FirebaseApp = app
}
