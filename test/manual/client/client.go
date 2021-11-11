package main

import (
	"crypto/tls"
	"log"

	"github.com/et-nik/binngo"
	"github.com/et-nik/binngo/decode"
)

func main() {
	log.SetFlags(log.Lshortfile)

	cer, err := tls.LoadX509KeyPair("./config/certs/client.crt", "./config/certs/client.key")
	if err != nil {
		panic(err)
	}

	conf := &tls.Config{
		Certificates:       []tls.Certificate{cer},
		InsecureSkipVerify: true,
	}

	conn, err := tls.Dial("tcp", "127.0.0.1:31717", conf)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	msg := []interface{}{0, "login", "password"}

	message, err := binngo.Marshal(msg)
	if err != nil {
		return
	}

	n, err := conn.Write(message)
	if err != nil {
		log.Println(n, err)
		return
	}

	var status []interface{}
	decoder := decode.NewDecoder(conn)
	err = decoder.Decode(&status)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println(msg)
}
