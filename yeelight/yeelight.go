// Package yeelight controls Yeelight WiFi lights over the LAN using the Yeelight
// Inter-Operation protocol. "LAN Control" must be enabled on each device.
package yeelight

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lsongdev/ssdp-go/ssdp"
)

const (
	// DefaultPort is the control port used when Config.Port is zero.
	DefaultPort = 55443
	// DefaultTimeout bounds a whole command when Config.Timeout is zero.
	DefaultTimeout = 3 * time.Second

	discoveryPort = 1982
	searchTarget  = "wifi_bulb"

	minDuration = 30 // milliseconds; the protocol's minimum for smooth effects
)

// Effects accepted by Effect.Effect.
const (
	EffectSudden = "sudden"
	EffectSmooth = "smooth"
)

// ErrNoDevices is returned by Find when discovery completes without a device.
var ErrNoDevices = errors.New("yeelight: no devices found")

type Config struct {
	Host string
	Port int
	// Timeout bounds a whole command: connect, write and read.
	Timeout time.Duration
}

func (c *Config) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Yeelight is a handle to one device. It is safe for concurrent use: every
// command uses its own short-lived connection. Devices accept at most 4
// simultaneous connections and 60 commands per minute on each connection, with
// a total of 144 commands per minute across the LAN.
type Yeelight struct {
	config Config
	lastID atomic.Int64
}

type Command struct {
	ID     int           `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// CommandError is an error reported by the device. Use errors.As to inspect it.
type CommandError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("yeelight: command failed: code %d: %s", e.Code, e.Message)
}

// CommandResult represents response from Bulb device
type CommandResult struct {
	ID     int           `json:"id"`
	Result []interface{} `json:"result,omitempty"`
	Error  *CommandError `json:"error,omitempty"`
}

// Effect describes how a change is applied. A nil *Effect means EffectSudden.
// Duration is in milliseconds, is at least 30 for EffectSmooth and is ignored
// by the device for EffectSudden.
type Effect struct {
	Effect   string `json:"effect"`
	Duration int    `json:"duration"`
}

type Color struct {
	Red   int `json:"r"`
	Green int `json:"g"`
	Blue  int `json:"b"`
}

func (c *Color) Int() int {
	return c.Red*65536 + c.Green*256 + c.Blue
}

func (c *Color) String() string {
	return fmt.Sprintf("#%02x%02x%02x", c.Red, c.Green, c.Blue)
}

type PowerMode int

const (
	Normal PowerMode = 0
	CT     PowerMode = 1
	RGB    PowerMode = 2
	HSV    PowerMode = 3
	Flow   PowerMode = 4
	Night  PowerMode = 5
)

// Discover searches the LAN until ctx ends or the search window elapses, and
// returns one handle per device. A context deadline simply ends the search. If
// the search fails or ctx is canceled, the devices found so far are returned
// along with the error.
func Discover(ctx context.Context) ([]*Yeelight, error) {
	return discover(ctx, ssdp.Config{Port: discoveryPort})
}

func discover(ctx context.Context, config ssdp.Config) ([]*Yeelight, error) {
	responses, err := ssdp.NewClient(config).Search(ctx, searchTarget)
	return lightsFromResponses(responses), err
}

func lightsFromResponses(responses []ssdp.Packet) []*Yeelight {
	var lights []*Yeelight
	seen := make(map[string]bool)
	for _, response := range responses {
		location := response.Header.Get("Location")
		u, err := url.Parse(location)
		if err != nil || u.Scheme != "yeelight" || u.Hostname() == "" {
			continue
		}
		// The protocol defines id as the device's unique identifier.
		key := response.Header.Get("id")
		if key == "" {
			key = location
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		port := 0
		if u.Port() != "" {
			if port, err = strconv.Atoi(u.Port()); err != nil {
				continue
			}
		}
		lights = append(lights, New(&Config{Host: u.Hostname(), Port: port}))
	}
	return lights
}

// Find returns the first device to respond, without waiting for the search to
// finish, or ErrNoDevices. With several devices on the LAN, which one responds
// first is unspecified.
func Find(ctx context.Context) (*Yeelight, error) {
	return find(ctx, ssdp.Config{Port: discoveryPort})
}

func find(ctx context.Context, config ssdp.Config) (*Yeelight, error) {
	var found *Yeelight
	err := ssdp.NewClient(config).SearchFunc(ctx, searchTarget, func(response ssdp.Packet) bool {
		if lights := lightsFromResponses([]ssdp.Packet{response}); len(lights) > 0 {
			found = lights[0]
			return true
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, ErrNoDevices
	}
	return found, nil
}

// New returns a handle for the configured device. Zero Port and Timeout values
// select DefaultPort and DefaultTimeout.
func New(config *Config) *Yeelight {
	y := &Yeelight{config: *config}
	if y.config.Port == 0 {
		y.config.Port = DefaultPort
	}
	if y.config.Timeout == 0 {
		y.config.Timeout = DefaultTimeout
	}
	return y
}

func (y *Yeelight) newCommand(method string, params []interface{}) *Command {
	if len(params) == 0 {
		params = []interface{}{}
	}
	return &Command{
		ID:     int(y.lastID.Add(1)),
		Method: method,
		Params: params,
	}
}

// Call sends any protocol method, including those without a dedicated helper
// such as start_cf or set_scene, and waits for its result. The call ends at
// Config.Timeout or when ctx ends, whichever comes first. A failure reported by
// the device is returned as a *CommandError.
func (y *Yeelight) Call(ctx context.Context, method string, params ...interface{}) (*CommandResult, error) {
	return y.execute(ctx, y.newCommand(method, params))
}

func (y *Yeelight) execute(ctx context.Context, cmd *Command) (*CommandResult, error) {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("yeelight: encode %s: %w", cmd.Method, err)
	}
	ctx, cancel := context.WithTimeout(ctx, y.config.Timeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", y.config.Address())
	if err != nil {
		return nil, fmt.Errorf("yeelight: connect to %s: %w", y.config.Address(), err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if _, err := conn.Write(append(payload, '\r', '\n')); err != nil {
		return nil, fmt.Errorf("yeelight: send %s: %w", cmd.Method, contextError(ctx, err))
	}
	out, err := readResult(conn, cmd.ID)
	if err != nil {
		var commandErr *CommandError
		if errors.As(err, &commandErr) {
			return nil, err
		}
		return nil, fmt.Errorf("yeelight: read %s result: %w", cmd.Method, contextError(ctx, err))
	}
	return out, nil
}

// contextError prefers the context's error, which explains an I/O failure
// caused by the deadline or cancellation interrupting the connection.
func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// readResult returns the result for id. Devices push a "props" notification to
// every connection on each state change, so these can arrive before the result.
func readResult(r io.Reader, id int) (*CommandResult, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var message struct {
			CommandResult
			Method string `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return nil, fmt.Errorf("parse %q: %w", scanner.Text(), err)
		}
		if message.Method != "" {
			continue
		}
		// Only one command is outstanding on a connection, so any error
		// belongs to it, even if the device did not echo the id.
		if message.Error != nil {
			return nil, message.Error
		}
		if message.ID == id {
			return &message.CommandResult, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

// executeCommand executes command with provided parameters
func (y *Yeelight) executeCommand(name string, params ...interface{}) (*CommandResult, error) {
	return y.Call(context.Background(), name, params...)
}

func effectParams(effect *Effect) ([]interface{}, error) {
	if effect == nil {
		return []interface{}{EffectSudden, minDuration}, nil
	}
	switch effect.Effect {
	case EffectSudden:
	case EffectSmooth:
		if effect.Duration < minDuration {
			return nil, fmt.Errorf("yeelight: smooth duration %dms is below the %dms minimum", effect.Duration, minDuration)
		}
	default:
		return nil, fmt.Errorf("yeelight: unknown effect %q", effect.Effect)
	}
	return []interface{}{effect.Effect, effect.Duration}, nil
}

func checkRange(name string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("yeelight: %s %d out of range %d-%d", name, value, min, max)
	}
	return nil
}

// setWithEffect runs a method whose parameters end with effect and duration.
func (y *Yeelight) setWithEffect(method string, effect *Effect, params ...interface{}) (*CommandResult, error) {
	suffix, err := effectParams(effect)
	if err != nil {
		return nil, err
	}
	return y.executeCommand(method, append(params, suffix...)...)
}

// GetProp returns the raw get_prop result. See GetProps for a simpler form.
func (y *Yeelight) GetProp(names ...interface{}) (*CommandResult, error) {
	return y.executeCommand("get_prop", names...)
}

// GetProps reads several properties in one command. A property the device does
// not support has an empty value.
func (y *Yeelight) GetProps(names ...string) (map[string]string, error) {
	params := make([]interface{}, len(names))
	for i, name := range names {
		params[i] = name
	}
	result, err := y.GetProp(params...)
	if err != nil {
		return nil, err
	}
	if len(result.Result) != len(names) {
		return nil, fmt.Errorf("yeelight: get_prop returned %d values for %d properties", len(result.Result), len(names))
	}
	props := make(map[string]string, len(names))
	for i, name := range names {
		switch value := result.Result[i].(type) {
		case string:
			props[name] = value
		case float64:
			props[name] = strconv.FormatFloat(value, 'f', -1, 64)
		default:
			props[name] = fmt.Sprint(value)
		}
	}
	return props, nil
}

func (y *Yeelight) SetName(name string) (*CommandResult, error) {
	return y.executeCommand("set_name", name)
}

// SetPower sets state to "on" or "off". A nil effect applies the change at once.
func (y *Yeelight) SetPower(state string, effect *Effect, mode PowerMode) (*CommandResult, error) {
	if state != "on" && state != "off" {
		return nil, fmt.Errorf("yeelight: invalid power state %q", state)
	}
	if err := checkRange("power mode", int(mode), int(Normal), int(Night)); err != nil {
		return nil, err
	}
	params, err := effectParams(effect)
	if err != nil {
		return nil, err
	}
	params = append([]interface{}{state}, params...)
	// mode is optional and Normal is its default.
	if mode != Normal {
		params = append(params, mode)
	}
	return y.executeCommand("set_power", params...)
}

func (y *Yeelight) Toggle() (*CommandResult, error) {
	return y.executeCommand("toggle")
}

// SetRGB sets the color as 0xRRGGBB. The device must be on.
func (y *Yeelight) SetRGB(color int, effect *Effect) (*CommandResult, error) {
	if err := checkRange("color", color, 0, 0xffffff); err != nil {
		return nil, err
	}
	return y.setWithEffect("set_rgb", effect, color)
}

// SetHSV sets hue (0-359) and saturation (0-100). The device must be on.
func (y *Yeelight) SetHSV(hue int, saturation int, effect *Effect) (*CommandResult, error) {
	if err := checkRange("hue", hue, 0, 359); err != nil {
		return nil, err
	}
	if err := checkRange("saturation", saturation, 0, 100); err != nil {
		return nil, err
	}
	return y.setWithEffect("set_hsv", effect, hue, saturation)
}

// SetCT sets the color temperature (1700-6500K). The device must be on.
func (y *Yeelight) SetCT(temperature int, effect *Effect) (*CommandResult, error) {
	if err := checkRange("color temperature", temperature, 1700, 6500); err != nil {
		return nil, err
	}
	return y.setWithEffect("set_ct_abx", effect, temperature)
}

// SetBright sets the brightness (1-100). The device must be on.
func (y *Yeelight) SetBright(brightness int, effect *Effect) (*CommandResult, error) {
	if err := checkRange("brightness", brightness, 1, 100); err != nil {
		return nil, err
	}
	return y.setWithEffect("set_bright", effect, brightness)
}

// GetHost returns the host address of the Yeelight device
func (y *Yeelight) GetHost() string {
	return y.config.Host
}

// GetPort returns the port of the Yeelight device
func (y *Yeelight) GetPort() int {
	return y.config.Port
}
