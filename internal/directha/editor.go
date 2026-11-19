package directha

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

//go:embed web/index.html web/app.js web/style.css
var editorAssets embed.FS

var ErrConflict = errors.New("configuration changed; reload before saving")
var ErrInvalidRules = errors.New("invalid command rules")

type RulesDocument struct {
	Revision string `json:"revision"`
	Rules    []Rule `json:"rules"`
}
type EditorStore struct {
	Read func() (RulesDocument, error)
	Save func(string, []Rule) (RulesDocument, error)
}

// NewEditor is a separate LAN-only UI; it does not expose the voice proxy or
// device-action endpoints. Mutations require same-origin JSON and a page token.
func NewEditor(controller *Controller, store EditorStore) (http.Handler, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	csrf := hex.EncodeToString(secret)
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, status int, message string) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	respond := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(value)
	}
	for route, path := range map[string]string{"GET /{$}": "web/index.html", "GET /assets/app.js": "web/app.js", "GET /assets/style.css": "web/style.css"} {
		data, err := editorAssets.ReadFile(path)
		if err != nil {
			return nil, err
		}
		contentType := "text/html; charset=utf-8"
		if strings.HasSuffix(path, ".js") {
			contentType = "text/javascript; charset=utf-8"
		}
		if strings.HasSuffix(path, ".css") {
			contentType = "text/css; charset=utf-8"
		}
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(data)
		})
	}
	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		doc, err := store.Read()
		if err != nil {
			fail(w, 500, "Не удалось прочитать настройки команд.")
			return
		}
		respond(w, struct {
			RulesDocument
			CSRFToken string `json:"csrf_token"`
		}{doc, csrf})
	})
	mux.HandleFunc("GET /api/entities", func(w http.ResponseWriter, r *http.Request) {
		entities, err := controller.Entities(r.Context())
		if err != nil {
			fail(w, 502, "Не удалось получить сущности из Home Assistant. Существующие привязки можно редактировать вручную.")
			return
		}
		respond(w, map[string]any{"entities": entities})
	})
	mux.HandleFunc("PUT /api/rules", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
			fail(w, 403, "Обновите страницу перед сохранением.")
			return
		}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "application/json" {
			fail(w, 415, "Ожидается JSON.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var doc RulesDocument
		if decoder.Decode(&doc) != nil || doc.Revision == "" {
			fail(w, 400, "Некорректный список команд.")
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			fail(w, 400, "Некорректный список команд.")
			return
		}
		next, err := store.Save(doc.Revision, doc.Rules)
		switch {
		case errors.Is(err, ErrConflict):
			fail(w, 409, "Настройки изменились в другой вкладке или вне редактора. Загрузите их заново перед сохранением.")
		case errors.Is(err, ErrInvalidRules):
			fail(w, 400, "Проверьте команды: фразы не должны повторяться, сущности и действия должны быть заполнены.")
		case err != nil:
			fail(w, 500, "Не удалось сохранить команды. Действующие правила сохранены.")
		default:
			respond(w, next)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		hostIP := net.ParseIP(strings.Trim(host, "[]"))
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		remoteIP := net.ParseIP(remote)
		local := func(ip net.IP) bool { return ip != nil && (ip.IsPrivate() || ip.IsLoopback()) }
		if err != nil || !local(remoteIP) || (!local(hostIP) && host != "localhost") {
			fail(w, 403, "Редактор доступен только по локальному IP-адресу.")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.User != nil || u.Host != r.Host || u.Scheme != scheme || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
				fail(w, 403, "Запрос с другого сайта запрещён.")
				return
			}
		} else if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 403, "Обновите страницу перед сохранением.")
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			fail(w, 403, "Запрос с другого сайта запрещён.")
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}
