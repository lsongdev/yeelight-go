package yeelight

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/lsongdev/ssdp-go/ssdp"
)

type Config struct {
	Host    string
	Port    int
	Timeout time.Duration
}

func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type Yeelight struct {
	config *Config
}

type Command struct {
	ID     int           `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// CommandResult represents response from Bulb device
type CommandResult struct {
	ID     int           `json:"id"`
	Result []interface{} `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

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

// discover all devices
func Discover() (lights []*Yeelight, err error) {
	discovery := ssdp.NewClient(&ssdp.Config{
		Port: 1982,
	})
	responses, err := discovery.Search("wifi_bulb")
	if err != nil {
		return
	}
	for _, response := range responses {
		u, err := url.Parse(response.Location)
		if err != nil {
			continue
		}
		p, _ := strconv.Atoi(u.Port())
		y := New(&Config{
			Host: u.Hostname(),
			Port: p,
		})
		lights = append(lights, y)
	}
	return
}

// find one device
func Find() (*Yeelight, error) {
	lights, err := Discover()
	if err != nil {
		return nil, err
	}
	return lights[0], nil
}

func New(config *Config) *Yeelight {
	if config.Timeout == 0 {
		config.Timeout = 3 * time.Second
	}
	return &Yeelight{
		config: config,
	}
}

func (y *Yeelight) newID() int {
	return rand.Intn(100)
}

func (y *Yeelight) newCommand(method string, params []interface{}) *Command {
	if len(params) == 0 {
		params = []interface{}{}
	}
	return &Command{
		ID:     y.newID(),
		Method: method,
		Params: params,
	}
}

func (y *Yeelight) execute(cmd *Command) (out *CommandResult, err error) {
	conn, err := net.Dial("tcp", y.config.Address())
	if nil != err {
		return nil, fmt.Errorf("cannot open connection to %s. %s", y.config.Address(), err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(y.config.Timeout))
	b, _ := json.Marshal(cmd)
	log.Println(cmd.ID, string(b))
	fmt.Fprint(conn, string(b)+"\r\n")
	// wait and read for response
	res, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("cannot read command result %s", err)
	}
	err = json.Unmarshal([]byte(res), &out)
	if nil != err {
		return nil, fmt.Errorf("cannot parse command result %s, %s", err, res)
	}
	if nil != out.Error {
		return nil, fmt.Errorf("command execution error. Code: %d, Message: %s", out.Error.Code, out.Error.Message)
	}
	return
}

// executeCommand executes command with provided parameters
func (y *Yeelight) executeCommand(name string, params ...interface{}) (*CommandResult, error) {
	return y.execute(y.newCommand(name, params))
}

func (y *Yeelight) GetProp(names ...interface{}) (*CommandResult, error) {
	return y.executeCommand("get_prop", names...)
}

func (y *Yeelight) SetName(name string) (*CommandResult, error) {
	return y.executeCommand("set_name", name)
}

func (y *Yeelight) SetPower(state string, effect *Effect, mode PowerMode) (*CommandResult, error) {
	return y.executeCommand("set_power", state, effect.Effect, effect.Duration, mode)
}

func (y *Yeelight) Toggle() (*CommandResult, error) {
	return y.executeCommand("toggle")
}

func (y *Yeelight) SetRGB(color int, effect *Effect) (*CommandResult, error) {
	return y.executeCommand("set_rgb", color, effect.Effect, effect.Duration)
}

func (y *Yeelight) SetHSV(hue int, saturation int, effect *Effect) (*CommandResult, error) {
	return y.executeCommand("set_hsv", hue, saturation, effect.Effect, effect.Duration)
}

func (y *Yeelight) SetCT(temperature int, effect *Effect) (*CommandResult, error) {
	return y.executeCommand("set_ct_abx", temperature, effect.Effect, effect.Duration)
}

func (y *Yeelight) SetBright(brightness int, effect *Effect) (*CommandResult, error) {
	return y.executeCommand("set_bright", brightness, effect.Effect, effect.Duration)
}
