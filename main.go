package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"

	"golang.org/x/net/context"

	"github.com/gorilla/mux"
	"github.com/robfig/cron"
)

func runScript() (string, error) {
	cmd := exec.Command("/bin/sh", "/scripts/entrypoint.sh")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

func runScriptOnSchedule() {
	output, err := runScript()
	if err != nil {
		log.Println(output, err)
	}
}

func forward(conn net.Conn, path string) {
	client, err := net.Dial("unix", path)
	if err != nil {
		log.Fatalf("Dial failed: %v", err)
	}

	go func() {
		defer client.Close()
		defer conn.Close()
		io.Copy(client, conn)
	}()
	go func() {
		defer client.Close()
		defer conn.Close()
		io.Copy(conn, client)
	}()
}

func main() {

	// Parse options from the command line
	socketPath := flag.String("socket", "", "socket file path")
	configPath := flag.String("config-base", "", "configuration base path")
	cronSpec := flag.String("cron-spec", "", "cron specification (optional)")
	forwardSocketPath := flag.String("forward-socket", "", "path to Unix socket to forward to port 443")

	flag.Parse()

	if *socketPath == "" {
		log.Fatal("Please provide a socket path with --socket")
	}

	if *configPath == "" {
		log.Fatal("Please provide a configuration base path with --config-base")
	}

	err := os.Remove(*socketPath)
	if err == nil {
		fmt.Println("Removed old socket file")
	}

	if *cronSpec != "" {
		c := cron.New()
		err = c.AddFunc(*cronSpec, runScriptOnSchedule)
		if err != nil {
			log.Fatal(err)
		}
		c.Start()
	}

	rtr := mux.NewRouter()
	rtr.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		output, err := runScript()
		if err != nil {
			http.Error(w, output, 500)
		} else {
			fmt.Fprintf(w, "ok\n")
		}
	}).Methods("POST")

	rtr.HandleFunc("/config/{key:[a-z]+}", func(w http.ResponseWriter, r *http.Request) {
		params := mux.Vars(r)
		key := params["key"]

		fileName := path.Join(*configPath, key)

		switch r.Method {
		case http.MethodGet:
			data, err := ioutil.ReadFile(fileName)
			if err != nil {
				http.Error(w, "value does not exist", http.StatusNotFound)
				return
			}
			w.Write(data)
		case http.MethodPost:
			if r.ContentLength > 0 {
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "can't read body", http.StatusBadRequest)
					return
				}
				ioutil.WriteFile(fileName, body, 0644)
			} else {
				file, _ := os.OpenFile(fileName, os.O_RDONLY|os.O_CREATE, 0644)
				defer file.Close()
			}
		case http.MethodDelete:
			err := os.Remove(fileName)
			if err != nil {
				http.Error(w, "value does not exist", http.StatusNotFound)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}).Methods("GET", "POST", "DELETE")

	http.Handle("/", rtr)

	server := http.Server{}

	unixListener, err := net.Listen("unix", *socketPath)
	if err != nil {
		log.Fatal(err)
	}

	// Don't grant permissions to 'others', since all 'internal' (i.e. non-nginx) apps should
	// belong to group 'carbon'
	if err := os.Chmod(*socketPath, 0660); err != nil {
		log.Fatal(err)
	}

	// As we're running as root, change group to carbon (1000)
	if err := os.Chown(*socketPath, -1, 1000); err != nil {
		log.Fatal(err)
	}

	if flag.NArg() > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		go runCmd(ctx, cancel, flag.Arg(0), flag.Args()[1:]...)
	}

	if *forwardSocketPath != "" {
		go func() {
			listener, err := net.Listen("tcp", "0.0.0.0:443")
			if err != nil {
				log.Fatalf("Failed to setup listener: %v", err)
			}

			for {
				conn, err := listener.Accept()
				if err != nil {
					log.Fatalf("ERROR: failed to accept listener: %v", err)
				}
				go forward(conn, *forwardSocketPath)
			}
		}()
	}

	// TODO: Make cancelable
	server.Serve(unixListener)
}
