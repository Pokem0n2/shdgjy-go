// shdgjy 进出场身份验证 — 数据接口（单二进制，零依赖）
// 行为与 PHP 版 (shdgjy-api.php) 完全一致：
//   GET  / → {"ok":true,"data":<appData>} 或 {"ok":false,"code":"nodata"}
//   POST / → body {"wk":"<64hex>","nw":"<64hex,可选>","d":<appData>}
//            首次无数据文件可初始化；之后 sha256(wk) 须匹配存储值；
//            有 nw 时写入后凭证切换为 nw。
// 数据文件由 SHDGJY_DATA 指定（默认 /data/shdgjy-data.json，容器挂载卷）。
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var (
	dataFile = envOr("SHDGJY_DATA", "/data/shdgjy-data.json")
	addr     = envOr("SHDGJY_ADDR", ":8443")
	maxBody  = int64(2 << 20) // 2MB
	wkRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type envelope struct {
	Data json.RawMessage `json:"data"`
	W    string          `json:"w"`
}

type reqBody struct {
	Wk string          `json:"wk"`
	Nw *string         `json:"nw"`
	D  json.RawMessage `json:"d"`
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func loadEnvelope() (*envelope, string) {
	raw, err := os.ReadFile(dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "nodata"
		}
		return nil, "readfail"
	}
	var e envelope
	if err := json.Unmarshal(raw, &e); err != nil || len(e.Data) == 0 {
		return nil, "corrupt"
	}
	return &e, ""
}

// 原子写：临时文件 + rename，避免写一半被读到
func saveEnvelope(e *envelope) error {
	dir := filepath.Dir(dataFile)
	tmp, err := os.CreateTemp(dir, ".shdgjy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	data, _ := json.Marshal(e)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil { // CreateTemp 默认 0600，放宽为常规可读（密文本身有加密保护）
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dataFile)
}

func handler(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case "OPTIONS":
		w.WriteHeader(http.StatusNoContent)
		return
	case "GET":
		e, code := loadEnvelope()
		if e == nil {
			if code == "nodata" {
				writeJSON(w, 200, map[string]interface{}{"ok": false, "code": "nodata"})
			} else {
				writeJSON(w, 500, map[string]interface{}{"ok": false, "code": code})
			}
			return
		}
		writeJSON(w, 200, map[string]interface{}{"ok": true, "data": e.Data})
	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		var req reqBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]interface{}{"ok": false, "code": "badreq"})
			return
		}
		if !wkRe.MatchString(req.Wk) || len(req.D) == 0 {
			writeJSON(w, 400, map[string]interface{}{"ok": false, "code": "badreq"})
			return
		}
		if req.Nw != nil && !wkRe.MatchString(*req.Nw) {
			writeJSON(w, 400, map[string]interface{}{"ok": false, "code": "badreq"})
			return
		}
		stored := sha256hex(req.Wk)
		if old, code := loadEnvelope(); old != nil {
			if subtle.ConstantTimeCompare([]byte(stored), []byte(old.W)) != 1 {
				log.Printf("WRITE denied")
				writeJSON(w, 403, map[string]interface{}{"ok": false, "code": "denied"})
				return
			}
		} else if code != "nodata" {
			writeJSON(w, 500, map[string]interface{}{"ok": false, "code": code})
			return
		}
		next := stored
		if req.Nw != nil {
			next = sha256hex(*req.Nw)
		}
		if err := saveEnvelope(&envelope{Data: req.D, W: next}); err != nil {
			log.Printf("WRITE fail: %v", err)
			writeJSON(w, 500, map[string]interface{}{"ok": false, "code": "writefail"})
			return
		}
		log.Printf("WRITE ok (%d bytes)", len(req.D))
		writeJSON(w, 200, map[string]interface{}{"ok": true})
	default:
		writeJSON(w, 405, map[string]interface{}{"ok": false, "code": "badmethod"})
	}
}

func main() {
	log.Printf("shdgjy-api listening on %s, data=%s", addr, dataFile)
	srv := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
