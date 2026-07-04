package backend

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cpolarBaseURL     = "https://dashboard.cpolar.com"
	cpolarConfigName  = "graphwar_cpolar_config.json"
	cpolarAccountNeed = 5
)

var (
	cpolarAddrRe = regexp.MustCompile(`\btcp://[^\s"'<>]+`)
	inputTagRe   = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	attrRe       = regexp.MustCompile(`(?is)\s([a-zA-Z0-9_\-:]+)\s*=\s*["']([^"']*)["']`)

	defaultCpolar = NewCpolarManager()
)

type CpolarAccount struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Token       string `json:"token"`
	CreatedAt   string `json:"createdAt"`
	LastLoginAt string `json:"lastLoginAt"`
	LastError   string `json:"lastError,omitempty"`
}

type CpolarConfig struct {
	BaseURL     string           `json:"baseUrl"`
	CpolarPath  string           `json:"cpolarPath,omitempty"`
	NextIndex   int              `json:"nextIndex"`
	Accounts    []CpolarAccount  `json:"accounts"`
	LastTunnels []CpolarSnapshot `json:"lastTunnels,omitempty"`
}

type CpolarSnapshot struct {
	Label       string `json:"label"`
	LocalPort   int    `json:"localPort"`
	LocalTarget string `json:"localTarget"`
	PublicURL   string `json:"publicUrl"`
	PublicHost  string `json:"publicHost"`
	PublicPort  int    `json:"publicPort"`
	StoppedAt   string `json:"stoppedAt"`
}

type CpolarTunnelInfo struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Proto         string   `json:"proto"`
	LocalPort     int      `json:"localPort"`
	LocalTarget   string   `json:"localTarget"`
	TunnelName    string   `json:"tunnelName"`
	PublicURL     string   `json:"publicUrl"`
	PublicHost    string   `json:"publicHost"`
	PublicPort    int      `json:"publicPort"`
	InspectAddr   string   `json:"inspectAddr,omitempty"`
	TargetChecked bool     `json:"targetChecked"`
	TargetError   string   `json:"targetError,omitempty"`
	Running       bool     `json:"running"`
	StartedAt     string   `json:"startedAt"`
	StoppedAt     string   `json:"stoppedAt,omitempty"`
	AccountEmail  string   `json:"accountEmail"`
	RecentLog     []string `json:"recentLog,omitempty"`
	LastError     string   `json:"lastError,omitempty"`
	ProcessExited bool     `json:"processExited"`
}

type CpolarStatus struct {
	OK           bool               `json:"ok"`
	ConfigPath   string             `json:"configPath"`
	CpolarPath   string             `json:"cpolarPath"`
	CpolarFound  bool               `json:"cpolarFound"`
	BaseURL      string             `json:"baseUrl"`
	AccountCount int                `json:"accountCount"`
	TokenCount   int                `json:"tokenCount"`
	NextIndex    int                `json:"nextIndex"`
	Tunnels      []CpolarTunnelInfo `json:"tunnels"`
	LastTunnels  []CpolarSnapshot   `json:"lastTunnels,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type CpolarManager struct {
	mu         sync.Mutex
	loaded     bool
	configPath string
	cfg        CpolarConfig
	tunnels    map[string]*cpolarTunnel
}

type cpolarTunnel struct {
	mu     sync.Mutex
	info   CpolarTunnelInfo
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewCpolarManager() *CpolarManager {
	return &CpolarManager{tunnels: map[string]*cpolarTunnel{}}
}

func DefaultCpolarManager() *CpolarManager { return defaultCpolar }

func (m *CpolarManager) ConfigPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configPath == "" {
		m.configPath = filepath.Join(cpolarConfigDir(), cpolarConfigName)
	}
	return m.configPath
}

func (m *CpolarManager) Status() CpolarStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.loadLocked()
	status := m.statusLocked()
	if err != nil {
		status.OK = false
		status.Error = err.Error()
	}
	return status
}

func (m *CpolarManager) InitAccounts() (CpolarStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return m.statusLocked(), err
	}
	if err := m.ensureAccountsLocked(cpolarAccountNeed); err != nil {
		st := m.statusLocked()
		st.Error = err.Error()
		return st, err
	}
	if err := m.saveLocked(); err != nil {
		st := m.statusLocked()
		st.Error = err.Error()
		return st, err
	}
	return m.statusLocked(), nil
}

func (m *CpolarManager) StartTCP(label string, localPort int) (CpolarTunnelInfo, error) {
	if localPort <= 0 || localPort > 65535 {
		return CpolarTunnelInfo{}, fmt.Errorf("invalid local port: %d", localPort)
	}
	return m.StartTCPForTarget(label, net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
}

func (m *CpolarManager) StartTCPForTarget(label, localTarget string) (CpolarTunnelInfo, error) {
	label = sanitizeCpolarLabel(label)
	localTarget, err := normalizeCpolarTarget(localTarget)
	if err != nil {
		return CpolarTunnelInfo{}, err
	}
	if localTarget == "" {
		return CpolarTunnelInfo{}, errors.New("local target is empty")
	}
	localPort := cpolarLocalPort(localTarget)
	if err := checkCpolarTarget(localTarget); err != nil {
		return CpolarTunnelInfo{}, err
	}

	m.mu.Lock()
	if err := m.loadLocked(); err != nil {
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	exePath, err := m.resolveCpolarPathLocked()
	if err != nil {
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	if err := m.ensureAccountsLocked(cpolarAccountNeed); err != nil {
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	acc, idx, err := m.pickAccountLocked()
	if err != nil {
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	m.cfg.NextIndex = (idx + 1) % len(m.cfg.Accounts)
	_ = m.saveLocked()

	id := "cp_" + randHex(8)
	tunnelName := sanitizeTunnelName("graphwar_" + label + "_" + randHex(4))
	startedAt := time.Now().Format(time.RFC3339)
	inspectAddr, inspectRelease, err := reserveCpolarInspectAddr()
	if err != nil {
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{
		"tcp",
		"-authtoken=" + acc.Token,
		"-inspect-addr=" + inspectAddr,
		"-tunnelName=" + tunnelName,
		"-log=stdout",
		"-log-level=INFO",
		localTarget,
	}
	cmd := exec.CommandContext(ctx, exePath, args...)
	hideChildWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		inspectRelease()
		cancel()
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		inspectRelease()
		cancel()
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	inspectRelease()
	t := &cpolarTunnel{
		info: CpolarTunnelInfo{
			ID: id, Label: label, Proto: "tcp", LocalPort: localPort, LocalTarget: localTarget,
			TunnelName: tunnelName, InspectAddr: inspectAddr, TargetChecked: true,
			Running: true, StartedAt: startedAt, AccountEmail: acc.Email,
		},
		cmd:    cmd,
		cancel: cancel,
	}
	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Unlock()
		return CpolarTunnelInfo{}, err
	}
	m.tunnels[id] = t
	m.mu.Unlock()

	go t.scan(stdout)
	go t.scan(stderr)
	go t.wait()

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		info := t.snapshot()
		if info.PublicURL != "" || info.LastError != "" || !info.Running {
			return info, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return t.snapshot(), nil
}

func (m *CpolarManager) Stop(id string) bool {
	m.mu.Lock()
	t := m.tunnels[id]
	m.mu.Unlock()
	if t == nil {
		return false
	}
	t.stop()
	m.rememberStopped(t.snapshot())
	return true
}

func (m *CpolarManager) StopAll() bool {
	m.mu.Lock()
	var tunnels []*cpolarTunnel
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.Unlock()
	for _, t := range tunnels {
		t.stop()
		m.rememberStopped(t.snapshot())
	}
	return len(tunnels) > 0
}

func (m *CpolarManager) rememberStopped(info CpolarTunnelInfo) {
	if info.PublicURL == "" && info.PublicHost == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.loadLocked()
	snap := CpolarSnapshot{
		Label: info.Label, LocalPort: info.LocalPort, LocalTarget: info.LocalTarget,
		PublicURL: info.PublicURL, PublicHost: info.PublicHost, PublicPort: info.PublicPort,
		StoppedAt: time.Now().Format(time.RFC3339),
	}
	m.cfg.LastTunnels = append([]CpolarSnapshot{snap}, m.cfg.LastTunnels...)
	if len(m.cfg.LastTunnels) > 10 {
		m.cfg.LastTunnels = m.cfg.LastTunnels[:10]
	}
	_ = m.saveLocked()
}

func (m *CpolarManager) loadLocked() error {
	if m.loaded {
		return nil
	}
	m.configPath = filepath.Join(cpolarConfigDir(), cpolarConfigName)
	m.cfg = CpolarConfig{BaseURL: cpolarBaseURL}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.loaded = true
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, &m.cfg); err != nil {
		return err
	}
	if strings.TrimSpace(m.cfg.BaseURL) == "" {
		m.cfg.BaseURL = cpolarBaseURL
	}
	m.loaded = true
	return nil
}

func (m *CpolarManager) saveLocked() error {
	if m.configPath == "" {
		m.configPath = filepath.Join(cpolarConfigDir(), cpolarConfigName)
	}
	if strings.TrimSpace(m.cfg.BaseURL) == "" {
		m.cfg.BaseURL = cpolarBaseURL
	}
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, m.configPath)
}

func (m *CpolarManager) statusLocked() CpolarStatus {
	exe, _ := m.resolveCpolarPathLocked()
	tokenCount := 0
	for _, acc := range m.cfg.Accounts {
		if strings.TrimSpace(acc.Token) != "" {
			tokenCount++
		}
	}
	var infos []CpolarTunnelInfo
	for _, t := range m.tunnels {
		infos = append(infos, t.snapshot())
	}
	return CpolarStatus{
		OK: true, ConfigPath: m.configPath, CpolarPath: exe, CpolarFound: exe != "",
		BaseURL: m.cfg.BaseURL, AccountCount: len(m.cfg.Accounts), TokenCount: tokenCount,
		NextIndex: m.cfg.NextIndex, Tunnels: infos, LastTunnels: m.cfg.LastTunnels,
	}
}

func (m *CpolarManager) ensureAccountsLocked(n int) error {
	var errs []string
	for len(m.cfg.Accounts) < n {
		acc := randomCpolarAccount()
		if err := cpolarRegisterAndToken(&acc, m.cfg.BaseURL); err != nil {
			acc.LastError = err.Error()
			errs = append(errs, acc.Email+": "+err.Error())
		}
		m.cfg.Accounts = append(m.cfg.Accounts, acc)
		_ = m.saveLocked()
	}
	for i := range m.cfg.Accounts {
		if strings.TrimSpace(m.cfg.Accounts[i].Token) != "" {
			continue
		}
		if err := cpolarLoginAndToken(&m.cfg.Accounts[i], m.cfg.BaseURL); err != nil {
			m.cfg.Accounts[i].LastError = err.Error()
			errs = append(errs, m.cfg.Accounts[i].Email+": "+err.Error())
		}
		_ = m.saveLocked()
	}
	if len(errs) > 0 && m.tokenCountLocked() == 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *CpolarManager) tokenCountLocked() int {
	n := 0
	for _, acc := range m.cfg.Accounts {
		if strings.TrimSpace(acc.Token) != "" {
			n++
		}
	}
	return n
}

func (m *CpolarManager) pickAccountLocked() (CpolarAccount, int, error) {
	if len(m.cfg.Accounts) == 0 {
		return CpolarAccount{}, 0, errors.New("no cpolar accounts configured")
	}
	start := m.cfg.NextIndex
	if start < 0 || start >= len(m.cfg.Accounts) {
		start = 0
	}
	for i := 0; i < len(m.cfg.Accounts); i++ {
		idx := (start + i) % len(m.cfg.Accounts)
		acc := m.cfg.Accounts[idx]
		if strings.TrimSpace(acc.Token) != "" {
			return acc, idx, nil
		}
	}
	return CpolarAccount{}, 0, errors.New("no cpolar authtoken available")
}

func (m *CpolarManager) resolveCpolarPathLocked() (string, error) {
	var candidates []string
	if m.cfg.CpolarPath != "" {
		candidates = append(candidates, m.cfg.CpolarPath)
	}
	if env := strings.TrimSpace(os.Getenv("CPOLAR_EXE")); env != "" {
		candidates = append(candidates, env)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "cpolar.exe"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "cpolar.exe"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if !filepath.IsAbs(c) && m.configPath != "" {
			c = filepath.Join(filepath.Dir(m.configPath), c)
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
	}
	return "", errors.New("cpolar.exe not found; place it next to GraphwarDesktopOpen.exe or set CPOLAR_EXE")
}

func cpolarConfigDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		lower := strings.ToLower(dir)
		if !strings.Contains(lower, "go-build") && !strings.Contains(lower, "go-build") {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func cpolarRegisterAndToken(acc *CpolarAccount, base string) error {
	client, err := newCpolarHTTPClient()
	if err != nil {
		return err
	}
	base = strings.TrimRight(defaultString(base, cpolarBaseURL), "/")
	signupURL := base + "/signup"
	page, err := httpGetText(client, signupURL, base+"/")
	if err != nil {
		return err
	}
	csrf := extractInputValue(page, "name", "csrf_token")
	if csrf == "" {
		return errors.New("signup csrf_token not found")
	}
	form := url.Values{
		"csrf_token":   {csrf},
		"email":        {acc.Email},
		"password":     {acc.Password},
		"name":         {acc.Name},
		"phone":        {acc.Phone},
		"inviteNumber": {""},
		"agreeTerms":   {"1"},
	}
	if _, err := httpPostFormText(client, signupURL, signupURL, form); err != nil {
		return err
	}
	acc.CreatedAt = time.Now().Format(time.RFC3339)
	return cpolarLoginAndTokenWithClient(client, acc, base)
}

func cpolarLoginAndToken(acc *CpolarAccount, base string) error {
	client, err := newCpolarHTTPClient()
	if err != nil {
		return err
	}
	return cpolarLoginAndTokenWithClient(client, acc, strings.TrimRight(defaultString(base, cpolarBaseURL), "/"))
}

func cpolarLoginAndTokenWithClient(client *http.Client, acc *CpolarAccount, base string) error {
	loginURL := base + "/login"
	page, err := httpGetText(client, loginURL, base+"/")
	if err != nil {
		return err
	}
	csrf := extractInputValue(page, "name", "csrf_token")
	if csrf == "" {
		return errors.New("login csrf_token not found")
	}
	form := url.Values{
		"csrf_token": {csrf},
		"login":      {acc.Email},
		"password":   {acc.Password},
	}
	body, err := httpPostFormText(client, loginURL, loginURL, form)
	if err != nil {
		return err
	}
	if strings.Contains(body, "name=\"password\"") && !strings.Contains(body, "/auth") {
		return errors.New("cpolar login did not reach dashboard")
	}
	auth, err := httpGetText(client, base+"/auth", base+"/")
	if err != nil {
		return err
	}
	token := extractInputValue(auth, "id", "authtoken")
	if token == "" {
		return errors.New("authtoken not found on /auth")
	}
	acc.Token = html.UnescapeString(token)
	acc.LastLoginAt = time.Now().Format(time.RFC3339)
	acc.LastError = ""
	return nil
}

func newCpolarHTTPClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{Jar: jar, Timeout: 25 * time.Second}, nil
}

func httpGetText(client *http.Client, url, referer string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	applyCpolarHeaders(req, referer)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func httpPostFormText(client *http.Client, rawURL, referer string, form url.Values) (string, error) {
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	applyCpolarHeaders(req, referer)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("POST %s returned %s", rawURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func applyCpolarHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func extractInputValue(page, attrName, attrValue string) string {
	for _, tag := range inputTagRe.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = html.UnescapeString(m[2])
		}
		if attrs[strings.ToLower(attrName)] == attrValue {
			return attrs["value"]
		}
	}
	return ""
}

func randomCpolarAccount() CpolarAccount {
	suffix := strings.ToLower(randHex(3))
	phoneDigits := strconv.Itoa(10000000 + int(randByte())*39062%89999999)
	if len(phoneDigits) > 8 {
		phoneDigits = phoneDigits[:8]
	}
	for len(phoneDigits) < 8 {
		phoneDigits += "0"
	}
	return CpolarAccount{
		Email:    "test_" + suffix + "@example.com",
		Password: "Password123!",
		Name:     "Graphwar_" + suffix,
		Phone:    "138" + phoneDigits,
	}
}

func (t *cpolarTunnel) scan(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		t.addLog(line)
		for _, addr := range cpolarAddrRe.FindAllString(line, -1) {
			t.setPublicURL(strings.TrimRight(addr, ".,);]"))
		}
	}
	if err := scanner.Err(); err != nil {
		t.setError(err.Error())
	}
}

func (t *cpolarTunnel) wait() {
	err := t.cmd.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.info.Running = false
	t.info.ProcessExited = true
	t.info.StoppedAt = time.Now().Format(time.RFC3339)
	if err != nil && t.info.LastError == "" {
		t.info.LastError = err.Error()
	}
}

func (t *cpolarTunnel) stop() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	proc := t.cmd.Process
	t.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
	}
}

func (t *cpolarTunnel) addLog(line string) {
	if line == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.info.RecentLog = append(t.info.RecentLog, line)
	if len(t.info.RecentLog) > 40 {
		t.info.RecentLog = t.info.RecentLog[len(t.info.RecentLog)-40:]
	}
}

func (t *cpolarTunnel) setError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.info.LastError = msg
}

func (t *cpolarTunnel) setPublicURL(addr string) {
	u, err := url.Parse(addr)
	if err != nil {
		return
	}
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.info.PublicURL == "" {
		t.info.PublicURL = addr
		t.info.PublicHost = host
		t.info.PublicPort = port
	}
}

func (t *cpolarTunnel) snapshot() CpolarTunnelInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := t.info
	if info.RecentLog != nil {
		info.RecentLog = append([]string{}, info.RecentLog...)
	}
	return info
}

func sanitizeCpolarLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "graphwar"
	}
	rs := []rune(s)
	if len(rs) > 32 {
		rs = rs[:32]
	}
	return string(rs)
}

func sanitizeTunnelName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "graphwar"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func cpolarLocalPort(target string) int {
	target = strings.TrimSpace(target)
	if p, err := strconv.Atoi(target); err == nil {
		return p
	}
	if _, p, err := netSplitHostPortLoose(target); err == nil {
		n, _ := strconv.Atoi(p)
		return n
	}
	return 0
}

func normalizeCpolarTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("local target is empty")
	}
	if p, err := strconv.Atoi(target); err == nil {
		if p <= 0 || p > 65535 {
			return "", fmt.Errorf("invalid local port: %d", p)
		}
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(p)), nil
	}
	host, portText, err := netSplitHostPortLoose(target)
	if err != nil {
		return "", fmt.Errorf("local target must be port or host:port: %s", target)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid local target port: %q", portText)
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func checkCpolarTarget(target string) error {
	conn, err := net.DialTimeout("tcp", target, 1200*time.Millisecond)
	if err != nil {
		return fmt.Errorf("local target %s is not reachable: %w", target, err)
	}
	_ = conn.Close()
	return nil
}

func reserveCpolarInspectAddr() (string, func(), error) {
	for port := 4042; port < 4092; port++ {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return addr, func() { _ = ln.Close() }, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", func() {}, fmt.Errorf("could not reserve a cpolar inspect port: %w", err)
	}
	addr := ln.Addr().String()
	return addr, func() { _ = ln.Close() }, nil
}

func netSplitHostPortLoose(s string) (string, string, error) {
	if strings.HasPrefix(s, "[") {
		return netSplitHostPort(s)
	}
	if strings.Count(s, ":") == 1 {
		i := strings.LastIndex(s, ":")
		return s[:i], s[i+1:], nil
	}
	return "", "", errors.New("not host:port")
}

func netSplitHostPort(s string) (string, string, error) {
	u, err := url.Parse("tcp://" + s)
	if err != nil {
		return "", "", err
	}
	return u.Hostname(), u.Port(), nil
}

func randHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UnixNano()
		return strconv.FormatInt(now, 16)
	}
	return hex.EncodeToString(buf)
}

func randByte() byte {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return byte(time.Now().UnixNano())
	}
	return b[0]
}

func defaultString(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
