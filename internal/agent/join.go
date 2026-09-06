package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/names"
)

var ErrNotEnrolled = errors.New("not enrolled")

type BindError struct {
	msg string
}

func (e *BindError) Error() string { return e.msg }

func bindErrorf(format string, args ...any) error {
	return &BindError{msg: fmt.Sprintf(format, args...)}
}

type EnrollRequest struct {
	V         int      `json:"v"`
	Token     string   `json:"token"`
	Name      string   `json:"name"`
	LoginUser string   `json:"login_user"`
	Pubkey    string   `json:"pubkey"`
	Tags      []string `json:"tags"`
}

type State struct {
	Name        string `json:"name"`
	Port        int    `json:"port"`
	TunnelUser  string `json:"tunnel_user"`
	VPSHostname string `json:"vps_hostname"`
	EnrolledAt  string `json:"enrolled_at"`
}

type enrollResponse struct {
	OK             json.RawMessage `json:"ok"`
	Name           string          `json:"name"`
	Port           int             `json:"port"`
	TunnelUser     string          `json:"tunnel_user"`
	VPSHostname    string          `json:"vps_hostname"`
	LoginUser      string          `json:"login_user"`
	KeyFingerprint string          `json:"key_fingerprint"`
	Error          string          `json:"error"`
	Message        string          `json:"message"`
}

func WriteEnrollRequest(p Paths, cfg config.Client, token string, force bool) (EnrollRequest, []byte, error) {
	token = strings.TrimSpace(token)
	if _, _, err := auth.Parse(token); err != nil {
		return EnrollRequest{}, nil, fmt.Errorf("invalid join token")
	}
	if !force {
		if _, err := os.Stat(p.StateFile()); err == nil {
			return EnrollRequest{}, nil, fmt.Errorf("already enrolled (%s exists); pass --force to write a new enroll request", p.StateFile())
		} else if !os.IsNotExist(err) {
			return EnrollRequest{}, nil, err
		}
	}
	name := strings.ToLower(strings.TrimSpace(cfg.Name))
	if err := names.Valid(name); err != nil {
		return EnrollRequest{}, nil, err
	}
	login := strings.TrimSpace(cfg.LoginUser)
	if err := names.ValidLoginUser(login); err != nil {
		return EnrollRequest{}, nil, err
	}
	if err := EnsureKey(p.IdentityFile(), KeyComment(name)); err != nil {
		return EnrollRequest{}, nil, err
	}
	pub, err := os.ReadFile(p.IdentityPub())
	if err != nil {
		return EnrollRequest{}, nil, err
	}
	pubLine := string(bytes.TrimSpace(pub))
	if _, err := auth.ParseEnrollPubkey([]byte(pubLine)); err != nil {
		return EnrollRequest{}, nil, fmt.Errorf("local pubkey: %w", err)
	}
	req := EnrollRequest{
		V:         1,
		Token:     token,
		Name:      name,
		LoginUser: login,
		Pubkey:    pubLine,
		Tags:      []string{},
	}
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return EnrollRequest{}, nil, err
	}
	raw = append(raw, '\n')
	if err := writeFileAtomic(p.EnrollRequest(), raw, 0o600); err != nil {
		return EnrollRequest{}, nil, err
	}
	return req, raw, nil
}

func ApplyResponse(p Paths, cfg config.Client, raw []byte) (State, error) {
	var resp enrollResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return State{}, bindErrorf("invalid enroll response JSON: %v", err)
	}
	if err := bindResponse(p, cfg, resp); err != nil {
		return State{}, err
	}
	st := State{
		Name:        strings.ToLower(strings.TrimSpace(resp.Name)),
		Port:        resp.Port,
		TunnelUser:  strings.TrimSpace(resp.TunnelUser),
		VPSHostname: strings.TrimSpace(resp.VPSHostname),
		EnrolledAt:  time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return State{}, err
	}
	body = append(body, '\n')
	if cfg.Server != "" {
		if _, _, _, err := config.ParseServer(cfg.Server); err != nil {
			return State{}, err
		}
	}
	if err := writeFileAtomic(p.StateFile(), body, 0o600); err != nil {
		return State{}, err
	}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		_ = os.Remove(p.StateFile())
		return State{}, err
	}
	if err := os.Remove(p.EnrollRequest()); err != nil && !os.IsNotExist(err) {
		return State{}, err
	}
	return st, nil
}

func bindResponse(p Paths, cfg config.Client, resp enrollResponse) error {
	if !okIsTrue(resp.OK) {
		if resp.Error != "" || resp.Message != "" {
			msg := strings.TrimSpace(resp.Error + ": " + resp.Message)
			msg = strings.Trim(msg, ": ")
			return bindErrorf("enroll refused: %s", msg)
		}
		return bindErrorf("enroll response ok is not true")
	}
	wantName := strings.ToLower(strings.TrimSpace(cfg.Name))
	gotName := strings.TrimSpace(resp.Name)
	if !strings.EqualFold(gotName, wantName) {
		return bindErrorf("name mismatch: response %q != config %q", gotName, cfg.Name)
	}
	fp, err := localFingerprint(p.IdentityPub())
	if err != nil {
		return err
	}
	gotFP := strings.TrimSpace(resp.KeyFingerprint)
	if gotFP != fp {
		return bindErrorf("key fingerprint mismatch: response %q != local %q", gotFP, fp)
	}
	if resp.Port < alloc.DefaultPortMin || resp.Port > alloc.DefaultPortMax {
		return bindErrorf("port %d out of range [%d,%d]", resp.Port, alloc.DefaultPortMin, alloc.DefaultPortMax)
	}
	tunnelUser := strings.TrimSpace(resp.TunnelUser)
	if tunnelUser == "" {
		return bindErrorf("tunnel_user is empty")
	}
	if err := names.ValidLoginUser(tunnelUser); err != nil {
		return bindErrorf("invalid tunnel_user %q", resp.TunnelUser)
	}
	vpsHost := strings.TrimSpace(resp.VPSHostname)
	if vpsHost == "" {
		return bindErrorf("vps_hostname is empty")
	}
	if err := names.ValidHostname(vpsHost); err != nil {
		return bindErrorf("invalid vps_hostname %q", resp.VPSHostname)
	}
	return nil
}

func okIsTrue(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return v
}

func localFingerprint(pubPath string) (string, error) {
	raw, err := os.ReadFile(pubPath)
	if err != nil {
		return "", err
	}
	key, err := auth.ParseEnrollPubkey(bytes.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("local pubkey: %w", err)
	}
	return auth.Fingerprint(key), nil
}

func LoadState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, err
	}
	return st, nil
}

func SaveState(path string, st State) error {
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeFileAtomic(path, body, 0o600)
}

func loadEnrolled(p Paths) (config.Client, State, error) {
	cfg, err := config.LoadClient(p.ConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return config.Client{}, State{}, fmt.Errorf("%w: client config not found (run postern config): %s", ErrNotEnrolled, p.ConfigFile())
		}
		return config.Client{}, State{}, err
	}
	st, err := LoadState(p.StateFile())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, State{}, fmt.Errorf("%w (%s missing); run postern join --apply-response first", ErrNotEnrolled, p.StateFile())
		}
		return cfg, State{}, err
	}
	if st.Port < 1 {
		return cfg, st, fmt.Errorf("port missing")
	}
	return cfg, st, nil
}

func SetName(p Paths, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := names.Valid(name); err != nil {
		return err
	}
	cfg, st, err := loadEnrolled(p)
	if err != nil {
		return err
	}
	cfg.Name = name
	st.Name = name
	if err := cfg.Save(p.ConfigFile()); err != nil {
		return err
	}
	if err := SaveState(p.StateFile(), st); err != nil {
		return err
	}
	return WriteSSHConfigs(p, st, cfg)
}
