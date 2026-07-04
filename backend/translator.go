package backend

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Translator ports the YouDao "灵动翻译" extension API (reverse-engineered in
// youdao_magic_api.py) to Go — only the text-translate path (sign generation +
// AES-CBC response decryption). No OCR/ASR (those need ONNX). The frontend
// can't call youdao directly (CORS), so the desktop app proxies via this.
type Translator struct {
	client    *http.Client
	secretKey string
}

const (
	ydPrefix    = "https://luna-ai.youdao.com"
	ydURLSecret = ydPrefix + "/extension/trans/secret"
	ydURLYD     = ydPrefix + "/extension/trans/yd"
	ydKeyID     = "ai-extension-trans"
	ydProduct   = "ai-extension-trans"
	ydPreKeyID  = "ai-extension-trans-pre"
	ydPreProd   = "ai-extension-trans-pre"
	ydPreSecret = "U2FsdGVkX18kMtp4uS6mPmglALZSexyDhlQuQ"
	ydRespKey   = "secret:/key/U2FsdGVkX19CnGVGgVHvKu83u66lpYNVbLms7sLVER7N/MaK2tG6Ax/YQqoFM4sA"
	ydRespIV    = "secret:/iv/GV@u19thDh/14HQ2Gjzk67OU5tTBHOGOTEJqPvKXJiTTIf7dcz0TSAZWcMV"
)

func NewTranslator() *Translator {
	return &Translator{client: &http.Client{Timeout: 20 * time.Second}}
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
func md5Bytes(s string) []byte {
	h := md5.Sum([]byte(s))
	return h[:]
}

// gen_sign: drop empty/nil, sort keys, append "key"=secret, md5 of k=v&...
func ydGenSign(data map[string]string, secret string) (string, string) {
	keys := make([]string, 0, len(data))
	for k, v := range data {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		parts = append(parts, k+"="+data[k])
	}
	parts = append(parts, "key="+secret)
	pointParam := strings.Join(append(append([]string{}, keys...), "key"), ",")
	return md5Hex(strings.Join(parts, "&")), pointParam
}

func ydGenParam(req map[string]string, secret, keyID, product string) map[string]string {
	d := map[string]string{}
	for k, v := range req {
		d[k] = v
	}
	d["product"] = product
	d["appVersion"] = "1"
	d["client"] = "web"
	d["mid"] = "1"
	d["vendor"] = "web"
	d["screen"] = "1"
	d["model"] = "1"
	d["imei"] = "1"
	d["network"] = "wifi"
	d["keyid"] = keyID
	d["mysticTime"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	d["yduuid"] = "abcdefg"
	sign, pointParam := ydGenSign(d, secret)
	d["sign"] = sign
	d["pointParam"] = pointParam
	return d
}

// JS encodeURIComponent-like quoting used by the extension (_quote_text).
func ydQuoteText(text string) string {
	text = strings.ReplaceAll(text, "+", " ")
	// url.QueryEscape encodes space as '+'; the extension keeps a set unescaped.
	q := url.QueryEscape(text)
	q = strings.ReplaceAll(q, "+", "%20")
	// unescape the safe set ~()*!.'
	repl := map[string]string{"%7E": "~", "%28": "(", "%29": ")", "%2A": "*", "%21": "!", "%2E": ".", "%27": "'"}
	for enc, dec := range repl {
		q = strings.ReplaceAll(q, enc, dec)
	}
	return q
}

func ydDecode(payload string) ([]byte, error) {
	raw := strings.TrimSpace(payload)
	raw = strings.Trim(raw, "\"")
	if raw == "" {
		return nil, fmt.Errorf("empty payload")
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(raw, "-", "+"), "_", "/")
	if pad := len(norm) % 4; pad != 0 {
		norm += strings.Repeat("=", 4-pad)
	}
	enc, err := base64.StdEncoding.DecodeString(norm)
	if err != nil {
		enc, err = base64.URLEncoding.DecodeString(raw + strings.Repeat("=", (4-len(raw)%4)%4))
		if err != nil {
			return nil, err
		}
	}
	block, err := aes.NewCipher(md5Bytes(ydRespKey))
	if err != nil {
		return nil, err
	}
	iv := md5Bytes(ydRespIV)
	mode := cipher.NewCBCDecrypter(block, iv)
	out := make([]byte, len(enc))
	mode.CryptBlocks(out, enc)
	// strip PKCS7 padding
	if len(out) == 0 {
		return nil, fmt.Errorf("decrypt empty")
	}
	padLen := int(out[len(out)-1])
	if padLen <= 0 || padLen > aes.BlockSize || padLen > len(out) {
		return nil, fmt.Errorf("bad padding")
	}
	return out[:len(out)-padLen], nil
}

func (t *Translator) ensureSecret() error {
	if t.secretKey != "" {
		return nil
	}
	params := ydGenParam(map[string]string{}, ydPreSecret, ydPreKeyID, ydPreProd)
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	resp, err := t.client.Get(ydURLSecret + "?" + q.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data struct {
			SecretKey string `json:"secretKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return err
	}
	if parsed.Data.SecretKey == "" {
		return fmt.Errorf("secretKey fetch failed")
	}
	t.secretKey = parsed.Data.SecretKey
	return nil
}

// Translate translates text to target language `to` (e.g. "zh-CHS", "en").
// Source language is auto-detected by YouDao. Returns the translated string.
func (t *Translator) Translate(text, to string) (string, error) {
	if to == "" {
		to = "zh-CHS"
	}
	if err := t.ensureSecret(); err != nil {
		return "", err
	}
	payload := map[string]string{"i": ydQuoteText(text), "to": to, "domain": "0"}
	signed := ydGenParam(payload, t.secretKey, ydKeyID, ydProduct)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range signed {
		_ = mw.WriteField(k, v)
	}
	mw.Close()
	resp, err := t.client.Post(ydURLYD, mw.FormDataContentType(), &buf)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	dec, err := ydDecode(string(body))
	if err != nil {
		// maybe it's already JSON (error path)
		return "", fmt.Errorf("decode failed: %v", err)
	}
	// `translation` may be a string or an array of strings depending on endpoint.
	var out struct {
		Translation json.RawMessage `json:"translation"`
	}
	if err := json.Unmarshal(dec, &out); err != nil {
		return "", err
	}
	if len(out.Translation) == 0 {
		return "", fmt.Errorf("no translation in response: %s", truncate(string(dec), 200))
	}
	var arr []string
	if json.Unmarshal(out.Translation, &arr) == nil {
		return strings.Join(arr, "\n"), nil
	}
	var single string
	if json.Unmarshal(out.Translation, &single) == nil {
		return single, nil
	}
	return "", fmt.Errorf("unexpected translation shape: %s", truncate(string(out.Translation), 200))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
