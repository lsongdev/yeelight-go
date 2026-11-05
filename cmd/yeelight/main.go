package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/lsongdev/yeelight-go/yeelight"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `Usage: yeelight <command> [args] [--host=value] [--port=value]

Commands:
  status          - Get current light status
  power           - Control power state: on, off, toggle
  brightness <n>  - Set brightness (1-100)
  color <rrggbb>  - Set color (e.g., ff0000 for red)
  temp <n>        - Set color temperature (1700-6500K)
  discover        - Discover Yeelight devices on the network
  help            - Show this help message

Options:
  --host=value    - Specify host IP address (default: discover first device)
  --port=value    - Specify port (default: 55443)

Examples:
  yeelight status
  yeelight power on
  yeelight power off
  yeelight power toggle
  yeelight brightness 50
  yeelight color ff0000
  yeelight discover
  yeelight status --host=192.168.1.100 --port=55443
`

var smooth = &yeelight.Effect{Effect: yeelight.EffectSmooth, Duration: 500}

// action runs a validated command against a device.
type action func(y *yeelight.Yeelight, out io.Writer) error

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	positional, host, port, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	command, commandArgs := positional[0], positional[1:]

	// Ctrl-C ends discovery early. It is released below because commands are
	// bounded by their own timeout and the default behavior should then apply.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch command {
	case "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "discover":
		if len(commandArgs) != 0 {
			return usageError(stderr, errors.New("discover takes no arguments"))
		}
		return report(stderr, discoverDevices(ctx, stdout))
	}

	// Validate before discovery, which can take several seconds.
	act, err := parseCommand(command, commandArgs)
	if err != nil {
		return usageError(stderr, err)
	}
	y, err := resolveDevice(ctx, host, port, stdout)
	if err != nil {
		return report(stderr, err)
	}
	stop()
	return report(stderr, act(y, stdout))
}

// parseArgs accepts flags anywhere, unlike a single flag.FlagSet.Parse, which
// stops at the first positional argument.
func parseArgs(args []string) (positional []string, host string, port int, err error) {
	fs := flag.NewFlagSet("yeelight", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&host, "host", "", "")
	fs.IntVar(&port, "port", 0, "")
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, "", 0, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, host, port, nil
}

func parseCommand(command string, args []string) (action, error) {
	switch command {
	case "status":
		if len(args) != 0 {
			return nil, errors.New("status takes no arguments")
		}
		return getStatus, nil
	case "power":
		if len(args) != 1 {
			return nil, errors.New("power command requires one action (on, off, toggle)")
		}
		switch args[0] {
		case "on", "off":
			state := args[0]
			return func(y *yeelight.Yeelight, out io.Writer) error { return setPower(y, out, state) }, nil
		case "toggle":
			return toggleLight, nil
		}
		return nil, fmt.Errorf("unknown power action '%s'. Use 'on', 'off', or 'toggle'", args[0])
	case "brightness":
		if len(args) != 1 {
			return nil, errors.New("brightness requires one value (1-100)")
		}
		brightness, err := strconv.Atoi(args[0])
		if err != nil || brightness < 1 || brightness > 100 {
			return nil, errors.New("brightness must be a number between 1 and 100")
		}
		return func(y *yeelight.Yeelight, out io.Writer) error {
			if _, err := y.SetBright(brightness, smooth); err != nil {
				return fmt.Errorf("failed to set brightness: %w", err)
			}
			fmt.Fprintf(out, "Brightness set to %d%%\n", brightness)
			return nil
		}, nil
	case "color":
		if len(args) != 1 {
			return nil, errors.New("color command requires one hex color value (e.g., ff0000)")
		}
		color, err := parseColor(args[0])
		if err != nil {
			return nil, err
		}
		return func(y *yeelight.Yeelight, out io.Writer) error {
			if _, err := y.SetRGB(color, smooth); err != nil {
				return fmt.Errorf("failed to set RGB color: %w", err)
			}
			fmt.Fprintf(out, "RGB color set to #%06x\n", color)
			return nil
		}, nil
	case "temp":
		if len(args) != 1 {
			return nil, errors.New("temp command requires one temperature value (1700-6500)")
		}
		temp, err := strconv.Atoi(args[0])
		if err != nil || temp < 1700 || temp > 6500 {
			return nil, fmt.Errorf("temperature must be a number between 1700 and 6500, got %q", args[0])
		}
		return func(y *yeelight.Yeelight, out io.Writer) error {
			if _, err := y.SetCT(temp, smooth); err != nil {
				return fmt.Errorf("failed to set color temperature: %w", err)
			}
			fmt.Fprintf(out, "Color temperature set to %dK\n", temp)
			return nil
		}, nil
	}
	return nil, fmt.Errorf("unknown command: %s", command)
}

func parseColor(value string) (int, error) {
	digits := strings.TrimPrefix(value, "#")
	color, err := strconv.ParseUint(digits, 16, 24)
	if err != nil || len(digits) != 6 {
		return 0, fmt.Errorf("invalid hex color: %s (expected rrggbb)", value)
	}
	return int(color), nil
}

func resolveDevice(ctx context.Context, host string, port int, out io.Writer) (*yeelight.Yeelight, error) {
	if host != "" {
		return yeelight.New(&yeelight.Config{Host: host, Port: port}), nil
	}
	found, err := yeelight.Find(ctx)
	if errors.Is(err, yeelight.ErrNoDevices) {
		return nil, errors.New("no Yeelight devices found on the network and no host specified")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to discover devices: %w", err)
	}
	if port == 0 {
		port = found.GetPort()
	}
	fmt.Fprintf(out, "Using discovered device: %s:%d\n", found.GetHost(), port)
	return yeelight.New(&yeelight.Config{Host: found.GetHost(), Port: port}), nil
}

func getStatus(y *yeelight.Yeelight, out io.Writer) error {
	props, err := y.GetProps("power", "bright", "color_mode", "ct")
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	temperature := "N/A"
	if props["ct"] != "" {
		temperature = props["ct"] + "K"
	}
	fmt.Fprintf(out, "Power status: %s\n", props["power"])
	fmt.Fprintf(out, "Brightness: %s%%\n", props["bright"])
	fmt.Fprintf(out, "Color mode: %s\n", props["color_mode"])
	fmt.Fprintf(out, "Color temperature: %s\n", temperature)
	if props["power"] == "on" {
		fmt.Fprintln(out, "The light is currently on")
	} else {
		fmt.Fprintln(out, "The light is currently off")
	}
	return nil
}

func setPower(y *yeelight.Yeelight, out io.Writer, state string) error {
	if _, err := y.SetPower(state, smooth, yeelight.Normal); err != nil {
		return fmt.Errorf("failed to set power to %s: %w", state, err)
	}
	fmt.Fprintf(out, "Light has been turned %s\n", state)
	return nil
}

func toggleLight(y *yeelight.Yeelight, out io.Writer) error {
	if _, err := y.Toggle(); err != nil {
		return fmt.Errorf("failed to toggle light state: %w", err)
	}
	fmt.Fprintln(out, "Light state toggled")
	return nil
}

func discoverDevices(ctx context.Context, out io.Writer) error {
	fmt.Fprintln(out, "Discovering Yeelight devices on the network...")
	lights, err := yeelight.Discover(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover devices: %w", err)
	}
	if len(lights) == 0 {
		fmt.Fprintln(out, "No Yeelight devices found on the network.")
		return nil
	}
	fmt.Fprintf(out, "Found %d Yeelight device(s):\n", len(lights))
	for i, light := range lights {
		props, err := light.GetProps("power", "bright")
		if err != nil {
			fmt.Fprintf(out, "%d. %s:%d - Error getting status: %v\n", i+1, light.GetHost(), light.GetPort(), err)
			continue
		}
		fmt.Fprintf(out, "%d. %s:%d - Power: %s, Brightness: %s%%\n", i+1, light.GetHost(), light.GetPort(), props["power"], props["bright"])
	}
	return nil
}

func report(stderr io.Writer, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return exitError
	}
	return exitOK
}

func usageError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "Error: %v\nRun 'yeelight help' for usage.\n", err)
	return exitUsage
}
