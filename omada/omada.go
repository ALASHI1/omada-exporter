package omada

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	defaultTimeout  = 30 * time.Second
	errTokenExpired = errors.New("token expired/unauthorized")
)

// Client connects to an Omada Controller.
type Client struct {
	logger     *zap.Logger
	http       *http.Client
	configPath string
	username   string
	password   string
	baseURL    string
	token      string
	mu         sync.Mutex
}

// NewClient returns a client that talks to an Omada Controller.
func NewClient(logger *zap.Logger, config *Config) (*Client, error) {
	transport := &http.Transport{}
	if !config.Secure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	c := Client{
		logger: logger,
		http: &http.Client{
			Transport: transport,
			Jar:       jar,
			Timeout:   defaultTimeout,
		},
		configPath: config.Path,
		baseURL:    strings.TrimSuffix(config.Path, "/"),
		username:   config.Username,
		password:   config.Password,
	}

	if err = c.authenticate(); err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *Client) BaseURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL
}

func (c *Client) SetBaseURL(path string) error {
	newPath, err := url.JoinPath(c.configPath, path)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.baseURL = strings.TrimSuffix(newPath, "/")
	c.mu.Unlock()

	return nil
}

func (c *Client) postJSON(url string, body io.Reader, target interface{}) error {
	req, err := http.NewRequest("POST", c.BaseURL()+url, body)
	if err != nil {
		return err
	}
	return c.doJSON(req, target)
}

func (c *Client) getJSON(url string, target interface{}) error {
	req, err := http.NewRequest("GET", c.BaseURL()+url, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, target)
}

func (c *Client) doJSON(req *http.Request, target interface{}) error {
	// Standard headers
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Csrf-Token", c.Token())

	// Inject cookies manually (fallback)
	u, _ := url.Parse(c.BaseURL())
	cookies := c.http.Jar.Cookies(u)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.ReadAll(res.Body)
		res.Body.Close()
	}()

	if res.StatusCode == http.StatusUnauthorized {
		return errTokenExpired
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%q returned %q", req.URL, res.Status)
	}

	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return err
	}

	return nil
}

func (c *Client) retryOnce(try func() error) error {
	retried := false
retry:
	err := try()
	if errors.Is(err, errTokenExpired) && !retried {
		retried = true
		if err = c.authenticate(); err != nil {
			return err
		}
		goto retry
	}
	return err
}

func (c *Client) authenticate() error {
	type infoResult struct {
		ErrorCode int64  `json:"errorCode"`
		Msg       string `json:"msg"`
		Result    struct {
			ControllerID string `json:"omadacId"`
		} `json:"result"`
	}

	c.SetToken("")
	if err := c.SetBaseURL("/"); err != nil {
		return err
	}

	var ir infoResult
	if err := c.getJSON("/api/info", &ir); err != nil {
		return err
	}

	if ir.Result.ControllerID == "" {
		return fmt.Errorf("missing controller ID: %v: %q", ir.ErrorCode, ir.Msg)
	}

	if err := c.SetBaseURL(ir.Result.ControllerID); err != nil {
		return err
	}

	type authResult struct {
		ErrorCode int64  `json:"errorCode"`
		Msg       string `json:"msg"`
		Result    struct {
			RoleType int64  `json:"roleType"`
			Token    string `json:"token"`
		} `json:"result"`
	}

	kv := map[string]string{
		"username": c.username,
		"password": c.password,
	}

	data, err := json.Marshal(kv)
	if err != nil {
		return err
	}

	var ar authResult
	err = c.postJSON("/api/v2/login", bytes.NewReader(data), &ar)
	if err != nil {
		return err
	}

	if ar.Result.Token == "" {
		return fmt.Errorf("auth failed: %v: %q", ar.ErrorCode, ar.Msg)
	}

	c.SetToken(ar.Result.Token)

	// Log cookies after login
	u, _ := url.Parse(c.BaseURL())
	for _, cookie := range c.http.Jar.Cookies(u) {
		c.logger.Info("Cookie after login", zap.String("name", cookie.Name), zap.String("value", cookie.Value))
	}

	return nil
}
