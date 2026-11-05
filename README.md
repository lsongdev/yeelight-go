# yeelight-go

> Golang API for Yeelight

## Install

```shell
go get github.com/lsongdev/yeelight-go
```

## CLI Usage

You can install a command-line interface for controlling Yeelight bulbs:

```shell
go install github.com/lsongdev/yeelight-go/cmd/yeelight@latest
```

### Commands

- `status` - Get current light status
- `power` - Control power state: on, off, toggle
- `brightness <n>` - Set brightness (1-100)
- `color <rrggbb>` - Set color (e.g., ff0000 for red)
- `temp <n>` - Set color temperature (1700-6500K)
- `discover` - Discover Yeelight devices on the network
- `help` - Show this help message

### Options

- `--host=value` - Specify host IP address (default: discover first device)
- `--port=value` - Specify port (default: 55443)

Options can appear before or after the command and its arguments. The exit code
is 0 on success, 1 if the command fails and 2 for invalid usage.

### Examples

```bash
# Get status of light at specific address
yeelight status --host=192.168.2.182 --port=55443

# Turn light on
yeelight power on --host=192.168.2.182 --port=55443

# Set brightness to 50%
yeelight brightness 50 --host=192.168.2.182 --port=55443

# Set color to red
yeelight color ff0000 --host=192.168.2.182 --port=55443

# Discover devices on network
yeelight discover

# Use first discovered device automatically (if only one device on network)
yeelight power on
```

## Library Usage

See the example in [example/main.go](example/main.go) for how to use the library programmatically.

Enable "LAN Control" on each device, otherwise it neither answers discovery nor
accepts commands.

```go
// A context deadline chooses the search window and is not an error.
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
lights, err := yeelight.Discover(ctx)

// A nil effect applies a change at once.
_, err = lights[0].SetBright(50, nil)
_, err = lights[0].SetRGB(0xff0000, &yeelight.Effect{Effect: yeelight.EffectSmooth, Duration: 500})

// Read several properties with one command.
props, err := lights[0].GetProps("power", "bright", "ct")

// Call any protocol method, and inspect errors reported by the device.
_, err = lights[0].Call(ctx, "set_scene", "ct", 3500, 50)
var deviceErr *yeelight.CommandError
if errors.As(err, &deviceErr) {
	log.Println(deviceErr.Code, deviceErr.Message)
}
```

## Node.js Implementation

> 💡 A Node.js Library for the Yeelight smart bulb
>
> <https://github.com/lsongdev/node-yeelight>

## License

MIT