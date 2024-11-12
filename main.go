package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"slices"

	"github.com/mholt/archiver/v4"
	"gopkg.in/yaml.v3"
)

//go:embed form.html
var f embed.FS

func main() {
	// обработка параметров
	if len(os.Args) < 2 {
		log.Fatal("в первом параметре должен быть передан путь к конфигу")
	}
	configGile := os.Args[1]

	// парсинг юшар
	data, err := os.ReadFile(configGile)
	if err != nil {
		log.Fatalf("не удалось открыть конфиг: %v", err)
	}

	config := ConfigFile{}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("не удалось спарсить yaml конфига: %v", err)
	}

	// обработка http запросов
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.ServeFileFS(w, r, f, "form.html")
			return
		}

		err = r.ParseForm()
		if err != nil {
			log.Printf("bad form: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		login := r.Form.Get("login")
		targetShare := r.Form.Get("share")
		pass := r.Form.Get("pass")

		// определить путь к юшаре
		ushareIndex := slices.IndexFunc(config.Ushares, func(share [2]string) bool {
			return share[0] == login
		})
		if ushareIndex == -1 {
			log.Printf("wrong ushare: %v", login)
			http.Error(w, "auth error 1", http.StatusForbidden)
			return
		}
		usharePath := config.Ushares[ushareIndex][1]

		// считать шары
		sharesFile := usharePath + "shares.yaml"
		data, err = os.ReadFile(sharesFile)
		if err != nil {
			log.Printf("cant open shares file: %v", err)
			http.Error(w, "bad shares", http.StatusInternalServerError)
			return
		}

		shares := UserSharesInfoFile{}
		err = yaml.Unmarshal(data, &shares)
		if err != nil {
			log.Printf("cant parse shares file: %v", err)
			http.Error(w, "bad shares format", http.StatusInternalServerError)
		}

		// определить шару
		shareIndex := slices.IndexFunc(shares.List, func(share UserShare) bool {
			return share.Name == targetShare
		})
		if shareIndex == -1 {
			log.Printf("wrong share: %v %v", login, targetShare)
			http.Error(w, "auth error 2", http.StatusForbidden)
			return
		}
		share := &shares.List[shareIndex]

		// проверить пароль
		passIndex := slices.IndexFunc(share.Passes, func(passInfo UserSharePassInfo) bool {
			return passInfo.Left != 0 && passInfo.Pass == pass
		})
		if passIndex == -1 {
			log.Printf("wrong pass: %v %v %v", login, targetShare, pass)
			http.Error(w, "auth error 3", http.StatusForbidden)
			return
		}
		passInfo := &share.Passes[passIndex]

		// отправка файла
		filesPath := usharePath + share.Path

		// -отправка одиночного файла
		fileInfo, err := os.Stat(filesPath)
		if err != nil {
			log.Printf("stat error: %v", err)
			http.Error(w, "cant stat files", http.StatusInternalServerError)
			return
		}

		if !fileInfo.IsDir() {
			_, file := path.Split(share.Path)

			w.Header().Set("Content-Disposition", "attachment; filename="+file)
			w.Header().Set("Content-Type", r.Header.Get("Content-Type"))

			http.ServeFile(w, r, filesPath)
			return
		}

		// -архивирование папки
		files, err := archiver.FilesFromDisk(nil, map[string]string{
			filesPath: "",
		})
		if err != nil {
			log.Printf("archiver scan error: %v", err)
			http.Error(w, "cant scan", http.StatusInternalServerError)
			return
		}

		format := archiver.CompressedArchive{
			Compression: archiver.Gz{},
			Archival:    archiver.Tar{},
		}

		w.Header().Set("Content-Disposition", "attachment; filename="+share.Name+".tar.gz")
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))

		err = format.Archive(context.Background(), w, files)
		if err != nil {
			log.Printf("archiver error: %v", err)
			http.Error(w, "cant archive", http.StatusInternalServerError)
			return
		}

		// поправить колво использований пароля
		if passInfo.Left > 0 {
			passInfo.Left--
		}

		data, err = yaml.Marshal(shares)
		if err != nil {
			log.Printf("cant marshal shares: %v", err)
			return
		}

		err = os.WriteFile(sharesFile, []byte(data), 0644)
		if err != nil {
			log.Printf("cant write shares: %v", err)
			return
		}
	})
	fmt.Printf("http server listening on %v\n", config.Listen)
	http.ListenAndServe(config.Listen, nil)
}

type ConfigFile struct {
	Listen  string
	Ushares [][2]string
}

type UserSharePassInfo struct {
	Pass string
	Left int
}

type UserShare struct {
	Name   string
	Path   string
	Passes []UserSharePassInfo
}

type UserSharesInfoFile struct {
	List []UserShare
}
