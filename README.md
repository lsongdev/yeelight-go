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

## Node.js Implementation

> 💡 A Node.js Library for the Yeelight smart bulb
>
> <https://github.com/lsongdev/node-yeelight>

## License

MIT