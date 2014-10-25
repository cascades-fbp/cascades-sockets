package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"

	"github.com/cascades-fbp/cascades/components/utils"
	"github.com/cascades-fbp/cascades/runtime"
	zmq "github.com/pebbe/zmq4"
)

var (
	// Flags
	optionsEndpoint = flag.String("port.options", "", "Component's options port endpoint")
	inputEndpoint   = flag.String("port.in", "", "Component's input port endpoint")
	outputEndpoint  = flag.String("port.out", "", "Component's output port endpoint")
	jsonFlag        = flag.Bool("json", false, "Print component documentation in JSON")
	debug           = flag.Bool("debug", false, "Enable debug mode")

	// Internal
	optionsPort, inPort, outPort *zmq.Socket
	err                          error
)

func validateArgs() {
	if *optionsEndpoint == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *inputEndpoint == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *outputEndpoint == "" {
		flag.Usage()
		os.Exit(1)
	}
}

func openPorts() {
	optionsPort, err = utils.CreateInputPort(*optionsEndpoint)
	utils.AssertError(err)

	inPort, err = utils.CreateInputPort(*inputEndpoint)
	utils.AssertError(err)
}

func closePorts() {
	optionsPort.Close()
	inPort.Close()
	if outPort != nil {
		outPort.Close()
	}
	zmq.Term()
}

func main() {
	flag.Parse()

	if *jsonFlag {
		doc, _ := registryEntry.JSON()
		fmt.Println(string(doc))
		os.Exit(0)
	}

	log.SetFlags(0)
	if *debug {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(ioutil.Discard)
	}

	validateArgs()

	openPorts()
	defer closePorts()

	utils.HandleInterruption()

	// Create service
	service := NewService()

	// Wait for the configuration on the options port
	log.Println("Waiting for configuration...")
	var bindAddr string
	for {
		ip, err := optionsPort.RecvMessageBytes(0)
		if err != nil {
			log.Println("Error receiving IP:", err.Error())
			continue
		}
		if !runtime.IsValidIP(ip) || !runtime.IsPacket(ip) {
			continue
		}
		bindAddr = string(ip[1])
		break
	}
	optionsPort.Close()

	// Create binding address listener
	laddr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		log.Fatalln(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("Listening on", listener.Addr())

	// Start server with listener
	go service.Serve(listener)
	go func() {
		outPort, err = utils.CreateOutputPort(*outputEndpoint)
		utils.AssertError(err)
		for data := range service.Output {
			outPort.SendMessage(runtime.NewOpenBracket())
			outPort.SendMessage(runtime.NewPacket(data[0]))
			outPort.SendMessage(runtime.NewPacket(data[1]))
			outPort.SendMessage(runtime.NewCloseBracket())
		}
	}()

	log.Println("Started...")
	var (
		connId string
		data   []byte
	)
	for {
		ip, err := inPort.RecvMessageBytes(0)
		if err != nil {
			log.Println("Error receiving message:", err.Error())
			continue
		}
		if !runtime.IsValidIP(ip) {
			continue
		}
		switch {
		case runtime.IsOpenBracket(ip):
			connId = ""
			data = nil
		case runtime.IsPacket(ip):
			if connId == "" {
				connId = string(ip[1])
			} else {
				data = ip[1]
			}
		case runtime.IsCloseBracket(ip):
			service.Dispatch(connId, data)
		}
	}
}
