package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	rate "golang.org/x/time/rate"
)

// Global Variables since it's going to be used a lot.
var origimg = canvas()
var cimg = image.NewRGBA(canvas().Bounds())

// This is used to ratelimit each user individually.
var rateLimits = make(map[string]*rate.Limiter)

type settings struct {
	Update int    `json:"update_duration_seconds"`
	Port   int    `json:"port"`
	Addr   string `json:"address"`
	Rlim   int    `json:"ratelimit"`
}

func main() {
	file, _ := os.Open("settings.json")
	defer file.Close()

	var set settings
	err := json.NewDecoder(file).Decode(&set)
	if err != nil {
		fmt.Println(err)
		return
	}

	img := canvas()
	go web(set.Port, set.Addr, set.Rlim) //Website operates async.
	fmt.Print("Website is being operated!\n")

	draw.Draw(cimg, img.Bounds(), img, image.Point{}, draw.Over)
	fmt.Print("Image has been created! \n")

	go frames(set.Update)
	fmt.Print("Frames system is up! \n")

	var user, action string
	var locX, locY, locX2, locY2 int
	var r, g, b uint8

	for {
		fmt.Print("$terminal =>")
		fmt.Scan(&user) //Admin or User

		if user == "user" {
			fmt.Print("Place pixel - X Y R G B ->")
			fmt.Scan(&locX, &locY, &r, &g, &b)
			pixelplace(locX, locY, r, g, b)
			fmt.Print("Pixel has been placed!\n")
		} else if user == "admin" {
			fmt.Print("$action =>") //rectangle
			fmt.Scan(&action)
			if action == "rectangle" {
				fmt.Print("Rectangle - X Y X2 Y2 R G B")
				fmt.Scan(&locX, &locY, &locX2, &locY2)
				rectangle(locX, locY, locX2, locY2)

			} else if action == "backup" { //Backs Up The canvas
				backup()
			} else {
				fmt.Print("Not approprate admin command.\n")
				continue
			}
		} else {
			fmt.Print("Inappropriate Response => Accepting only 'user' or 'admin' \n")
			continue
		}
	}
}

type info struct {
	R uint8 `json:"R"`
	G uint8 `json:"G"`
	B uint8 `json:"B"`
}

func getpixel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	queryParams := r.URL.Query()
	locX, err := strconv.Atoi(queryParams.Get("x"))
	if err != nil {
		http.Error(w, "Location X cannot be properly parsed.", http.StatusBadRequest)
		return
	}

	locY, err := strconv.Atoi(queryParams.Get("y"))
	if err != nil {
		http.Error(w, "Location Y cannot be properly parsed.", http.StatusBadRequest)
		return
	}

	if locX > cimg.Bounds().Max.X {
		http.Error(w, "X location is outside of Canvas range.", http.StatusForbidden)
		return
	}
	if locY > cimg.Bounds().Max.Y {
		http.Error(w, "Y location is outside of Canvas range.", http.StatusForbidden)
		return
	}

	re, g, b, _ := cimg.At(locX, locY).RGBA()

	info := info{R: uint8(re), G: uint8(g), B: uint8(b)}
	json.NewEncoder(w).Encode(info)

}

func homepage(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("For the canvas. Go to '<web-address:port>/canvas'. \n\nFor getting an individual pixel go to: '<web-address:port>/pixel?x=0&y=0'\n"))
}

// Website
type Payload struct {
	UInput []int `json:"data"`
}

func web(port int, addr string, ratelim int) {

	mux := http.NewServeMux()
	mux.HandleFunc("/", homepage)
	mux.HandleFunc("/pixel", getpixel)
	mux.HandleFunc("/canvas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r.Header.Set("Content-Type", "application/json")
			clientIP := strings.Split(r.RemoteAddr, ":")[0]

			// Check if we have a rate limiter for the client IP, create one if not
			if !rateLimits[clientIP].Allow() {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			} else if rateLimits[clientIP] == nil {
				print(clientIP)
				rateLimits[clientIP] = rate.NewLimiter(rate.Limit(ratelim), ratelim) //Ratelimits ratelim (default: 180) pixels per second per user of request.
			}

			var uin Payload
			if err := json.NewDecoder(r.Body).Decode(&uin); err != nil {
				http.Error(w, "Error decoding JSON payload", http.StatusBadRequest)
				return
			}

			if uin.UInput[0] > cimg.Bounds().Max.X {
				http.Error(w, "X location is outside of Canvas range.", http.StatusForbidden)
				return
			}
			if uin.UInput[1] > cimg.Bounds().Max.Y {
				http.Error(w, "Y location is outside of Canvas range.", http.StatusForbidden)
				return
			}

			go pixelplace(uin.UInput[0], uin.UInput[1], uint8(uin.UInput[2]), uint8(uin.UInput[3]), uint8(uin.UInput[4])) //LocX LocY R G B
			w.Write([]byte("Pixel successfully placed at: " + fmt.Sprint(uin.UInput[0]) + "," + fmt.Sprint(uin.UInput[1])))

		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "upgrade")
			w.Header().Set("Upgrade", "websocket")

			//If the user is NOT POST-ing then they will just see the picture of the canvas.
			png.Encode(w, sitecanvas())

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	})

	target := addr + ":" + strconv.Itoa(port)
	fmt.Print(target + "\n")
	log.Fatal(http.ListenAndServe(target, mux))
}

// Pixel Placing Mechanism
func pixelplace(locX int, locY int, R, G, B uint8) {
	cimg.Set(locX, locY, color.RGBA{uint8(R), uint8(G), uint8(B), 255})
}

// Admin Pixel Placing
func rectangle(lX, lY, lX2, lY2 int) {
	fmt.Print("Drawing White Recetangle... \n")
	rect := image.Rect(lX, lY, lX2, lY2)
	draw.Draw(cimg, rect, &image.Uniform{color.White}, image.Point{lX, lX2}, draw.Over)
	fmt.Print("Rectangle completed! \n")
}

// Canvas Updating - Constantly operating

func frames(delay int) {
	for {
		time.Sleep(time.Duration(delay) * time.Second)
		go update(cimg)
	}
}

// Canvas Manipulation and Data Fetching
func canvas() image.Image {
	canvas, _ := os.Open("canvas.png") //canvas = Main folder.
	img, _ := png.Decode(canvas)
	canvas.Close()

	outFile, _ := os.Create("main.png")
	png.Encode(outFile, img)
	outFile.Close()

	return img
}

func update(upimg *image.RGBA) {

	e := os.Remove("main.png")
	if e != nil {
		log.Fatal(e)
	}

	outFile, _ := os.Create("main.png")
	png.Encode(outFile, upimg)
	outFile.Close()
}

func sitecanvas() image.Image {
	//Create Image to merge.
	bounds := origimg.Bounds()
	newImg := image.NewRGBA(bounds)
	draw.Draw(newImg, bounds, origimg, image.Point{0, 0}, draw.Over)
	draw.Draw(newImg, bounds, cimg, image.Point{0, 0}, draw.Over)
	return newImg
}

func backup() {
	fmt.Print("Backing up main.png...\n")
	canvas, _ := os.Open("canvas.png") //canvas = Main folder
	img, _ := png.Decode(canvas)
	canvas.Close()

	art, _ := os.Open("main.png")
	artedits, _ := png.Decode(art)
	art.Close()

	//Create Image to merge.
	bounds := img.Bounds()
	newImg := image.NewRGBA(bounds)
	draw.Draw(newImg, bounds, img, image.Point{0, 0}, draw.Over)
	draw.Draw(newImg, bounds, artedits, image.Point{0, 0}, draw.Over)

	outFile, _ := os.Create("backup.png")
	png.Encode(outFile, newImg)
	outFile.Close()
	fmt.Print("Backup is complete. backup.png is made!\n")
}
