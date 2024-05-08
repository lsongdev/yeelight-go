package main

import (
	"log"
	"time"

	"github.com/lsongdev/yeelight-go/yeelight"
)

func main() {
	y, err := yeelight.Find()
	if err != nil {
		log.Fatal(err)
	}
	// y := yeelight.New(&yeelight.Config{
	// 	IP:   "192.168.2.182",
	// 	Port: 55443,
	// })

	result, err := y.GetProp("name")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("name:", result.Result[0])

	result, _ = y.SetName("Yeelight")
	log.Println(result)

	result, err = y.GetProp("power")
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Result[0])

	result, err = y.SetPower("on", &yeelight.Effect{Effect: "smooth", Duration: 1500}, 2)
	log.Println(result, err)

	result, err = y.SetBright(100, &yeelight.Effect{Effect: "smooth", Duration: 1500})
	log.Println(result, err)

	result, err = y.SetRGB(
		0xff0000,
		&yeelight.Effect{Effect: "smooth", Duration: 1500},
	)
	log.Println(result, err)
	time.Sleep(2 * time.Second)

	result, err = y.SetRGB(
		0x00ff00,
		&yeelight.Effect{Effect: "smooth", Duration: 1500},
	)
	log.Println(result, err)
	time.Sleep(2 * time.Second)

	result, err = y.SetRGB(
		0x0000ff,
		&yeelight.Effect{Effect: "smooth", Duration: 1500},
	)
	log.Println(result, err)
	time.Sleep(2 * time.Second)

	result, err = y.SetRGB(
		0xfffff,
		&yeelight.Effect{Effect: "smooth", Duration: 1500},
	)
	log.Println(result, err)
	result, err = y.SetBright(3, &yeelight.Effect{Effect: "smooth", Duration: 1500})
	log.Println(result, err)

	result, err = y.GetProp("bright")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("bright:", result.Result[0])
	time.Sleep(3 * time.Second)
	result, err = y.Toggle()
	log.Println(result, err)
}
