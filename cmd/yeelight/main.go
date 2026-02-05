package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/lsongdev/yeelight-go/internal/cli"
	"github.com/lsongdev/yeelight-go/yeelight"
)

func printUsage() {
	fmt.Println("Usage: yeelight <command> [args] [--host=value] [--port=value]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  status          - Get current light status")
	fmt.Println("  power           - Control power state: on, off, toggle")
	fmt.Println("  brightness <n>  - Set brightness (1-100)")
	fmt.Println("  color <rrggbb>  - Set color (e.g., ff0000 for red)")
	fmt.Println("  temp <n>        - Set color temperature (1700-6500K)")
	fmt.Println("  discover        - Discover Yeelight devices on the network")
	fmt.Println("  help            - Show this help message")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --host=value    - Specify host IP address (default: discover first device)")
	fmt.Println("  --port=value    - Specify port (default: 55443)")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  yeelight status")
	fmt.Println("  yeelight power on")
	fmt.Println("  yeelight power off")
	fmt.Println("  yeelight power toggle")
	fmt.Println("  yeelight brightness 50")
	fmt.Println("  yeelight color ff0000")
	fmt.Println("  yeelight discover")
	fmt.Println("  yeelight status --host=192.168.1.100 --port=55443")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	args, flags := cli.ParseArgs()

	if len(args) < 1 {
		printUsage()
		return
	}

	command := args[0]

	// Handle commands that don't require a device connection
	if command == "discover" || command == "help" || command == "-h" || command == "--help" {
		switch command {
		case "discover":
			discoverDevices()
		case "help":
			fallthrough
		case "-h":
			fallthrough
		case "--help":
			printUsage()
		}
		return
	}

	// Extract additional arguments for commands that need them
	var remainingArgs []string
	if len(args) > 1 {
		remainingArgs = args[1:]
	}

	// Get host and port from flags, or discover automatically
	var host string
	var port int

	// Check if host was provided as --host=value
	hostFlagValue, hostExists := flags["host"]
	if hostExists && hostFlagValue != true {
		host = hostFlagValue.(string)
	} else {
		// Discover the first available device
		lights, err := yeelight.Discover()
		if err != nil {
			log.Fatal("Failed to discover devices:", err)
		}
		if len(lights) == 0 {
			log.Fatal("No Yeelight devices found on the network and no host specified")
		}
		// Use the first discovered device
		host = lights[0].GetHost()
		port = lights[0].GetPort()
		fmt.Printf("Using discovered device: %s:%d\n", host, port)
	}

	// Get port from flags, default to 55443 if not specified
	portFlagValue, portExists := flags["port"]
	if portExists && portFlagValue != true {
		// Convert string value to int
		var portVal int
		switch v := portFlagValue.(type) {
		case string:
			p, err := strconv.Atoi(v)
			if err != nil {
				log.Fatal("Invalid port number:", err)
			}
			portVal = p
		case int:
			portVal = v
		default:
			log.Fatal("Port flag must be a number")
		}
		port = portVal
	} else {
		if port == 0 { // Only set default if port wasn't set from discovery
			port = 55443
		}
	}

	switch command {
	case "status":
		getStatus(host, port)
	case "power":
		if len(remainingArgs) < 1 {
			fmt.Println("Error: power command requires an action (on, off, toggle)")
			return
		}
		powerAction := remainingArgs[0]
		switch powerAction {
		case "on":
			setPower(host, port, "on")
		case "off":
			setPower(host, port, "off")
		case "toggle":
			toggleLight(host, port)
		default:
			fmt.Printf("Error: unknown power action '%s'. Use 'on', 'off', or 'toggle'\n", powerAction)
			return
		}
	case "brightness":
		if len(remainingArgs) < 1 {
			fmt.Println("Error: brightness requires a value (1-100)")
			return
		}
		brightness, err := strconv.Atoi(remainingArgs[0])
		if err != nil || brightness < 1 || brightness > 100 {
			fmt.Println("Error: brightness must be a number between 1 and 100")
			return
		}
		setBrightness(host, port, brightness)
	case "color":
		if len(remainingArgs) < 1 {
			fmt.Println("Error: color command requires a hex color value (e.g., ff0000)")
			return
		}
		colorStr := strings.TrimPrefix(remainingArgs[0], "#")
		colorInt, err := strconv.ParseInt(colorStr, 16, 0)
		if err != nil {
			fmt.Printf("Error: invalid hex color: %s\n", remainingArgs[0])
			return
		}
		setRGB(host, port, int(colorInt))
	case "temp":
		if len(remainingArgs) < 1 {
			fmt.Println("Error: temp command requires a temperature value (1700-6500)")
			return
		}
		temp, err := strconv.Atoi(remainingArgs[0])
		if err != nil || temp < 1700 || temp > 6500 {
			fmt.Printf("Error: temperature must be a number between 1700 and 6500, got %d\n", temp)
			return
		}
		setTemperature(host, port, temp)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("")
		printUsage()
	}
}

func getStatus(host string, port int) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	powerResult, err := y.GetProp("power")
	if err != nil {
		log.Fatal("Failed to get power status:", err)
	}

	brightResult, err := y.GetProp("bright")
	if err != nil {
		log.Fatal("Failed to get brightness:", err)
	}

	colorModeResult, err := y.GetProp("color_mode")
	if err != nil {
		log.Fatal("Failed to get color mode:", err)
	}

	ctResult, err := y.GetProp("ct")
	if err != nil {
		// CT may not be available, ignore error
		ctResult = &yeelight.CommandResult{Result: []interface{}{"N/A"}}
	}

	fmt.Printf("Power status: %s\n", powerResult.Result[0])
	fmt.Printf("Brightness: %s%%\n", brightResult.Result[0])
	fmt.Printf("Color mode: %s\n", colorModeResult.Result[0])
	fmt.Printf("Color temperature: %sK\n", ctResult.Result[0])

	if powerResult.Result[0] == "on" {
		fmt.Println("The light is currently on")
	} else {
		fmt.Println("The light is currently off")
	}
}

func setPower(host string, port int, state string) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	effect := &yeelight.Effect{
		Effect:   "smooth",
		Duration: 500,
	}
	
	result, err := y.SetPower(state, effect, yeelight.Normal)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to set power to %s:", state), err)
	}

	fmt.Printf("Light has been %s, response: %v\n", map[string]string{"on": "turned on", "off": "turned off"}[state], result.Result[0])
}

func toggleLight(host string, port int) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	result, err := y.Toggle()
	if err != nil {
		log.Fatal("Failed to toggle light state:", err)
	}

	fmt.Printf("Light state toggled, response: %v\n", result.Result[0])
}

func setBrightness(host string, port int, brightness int) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	effect := &yeelight.Effect{
		Effect:   "smooth",
		Duration: 500,
	}
	
	result, err := y.SetBright(brightness, effect)
	if err != nil {
		log.Fatal("Failed to set brightness:", err)
	}

	fmt.Printf("Brightness set to %d%%, response: %v\n", brightness, result.Result[0])
}

func setRGB(host string, port int, color int) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	effect := &yeelight.Effect{
		Effect:   "smooth",
		Duration: 500,
	}
	
	result, err := y.SetRGB(color, effect)
	if err != nil {
		log.Fatal("Failed to set RGB color:", err)
	}

	fmt.Printf("RGB color set to #%06x, response: %v\n", color, result.Result[0])
}

func setTemperature(host string, port int, temp int) {
	y := yeelight.New(&yeelight.Config{
		Host: host,
		Port: port,
	})

	effect := &yeelight.Effect{
		Effect:   "smooth",
		Duration: 500,
	}
	
	result, err := y.SetCT(temp, effect)
	if err != nil {
		log.Fatal("Failed to set color temperature:", err)
	}

	fmt.Printf("Color temperature set to %dK, response: %v\n", temp, result.Result[0])
}

func discoverDevices() {
	fmt.Println("Discovering Yeelight devices on the network...")
	
	lights, err := yeelight.Discover()
	if err != nil {
		log.Fatal("Failed to discover devices:", err)
	}

	if len(lights) == 0 {
		fmt.Println("No Yeelight devices found on the network.")
		return
	}

	fmt.Printf("Found %d Yeelight device(s):\n", len(lights))
	for i, light := range lights {
		// Get the device properties
		powerResult, err := light.GetProp("power")
		if err != nil {
			fmt.Printf("%d. %s:%d - Error getting status: %v\n", i+1, light.GetHost(), light.GetPort(), err)
			continue
		}
		
		brightResult, err := light.GetProp("bright")
		if err != nil {
			fmt.Printf("%d. %s:%d - Power: %s, Error getting brightness: %v\n", i+1, light.GetHost(), light.GetPort(), powerResult.Result[0], err)
			continue
		}
		
		fmt.Printf("%d. %s:%d - Power: %s, Brightness: %s%%\n", i+1, light.GetHost(), light.GetPort(), powerResult.Result[0], brightResult.Result[0])
	}
}