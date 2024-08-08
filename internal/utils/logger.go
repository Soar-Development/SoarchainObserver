package utils

import (
	"log"
	"os"
)

func GetLogger() *log.Logger {
	logger := log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	return logger
}
