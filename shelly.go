package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const debug = false
const appName = "shelly"

// const timeFormat = "2006-01-02 15:04:05"

func usage_onoff() {
	fmt.Printf("Usage: %s onoff <relays> <timerange>\n\n", appName)
	fmt.Println("  relays      Relay id or list of relay ids")
	fmt.Println("  timerange   Date/time range")
	fmt.Print("\nExamples:\n\n")
	fmt.Printf("  %s onoff 0,1,2 today 17..18\n", appName)
	fmt.Printf("  %s onoff 0 tomorrow 2..3\n", appName)
	fmt.Print("\n\n")
	fmt.Println("Note 1: by default, all earlier schedules are deleted before settings new ones.")
	fmt.Println("Note 2: an offset to time is set according to formula <relay_id>*10 seconds.")
}

func ParseInts(w string, sep string) ([]int, error) {
	strs := strings.Split(w, sep)
	res := []int{}
	for _, s := range strs {
		if debug {
			log.Printf("Parsing string '%s' to integer", s)
		}
		if s == "" {
			continue
		}
		val, err := strconv.Atoi(s)
		if err != nil {
			return res, errors.New("invalid integer value: " + s)
		}
		res = append(res, val)
	}
	return res, nil
}

func CheckConnection(uri string) error {
	uri2 := uri + "Shelly.GetStatus"
	log.Printf("Getting Shelly status from " + uri2)
	resp, err := http.Get(uri2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("Response status code: %d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return errors.New("status code != 200")
	}
	return nil
}

func ScheduleDeleteAll(uri string) error {
	log.Printf("Removing old schedules ... ")
	resp, err := http.Get(uri + "Schedule.DeleteAll")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		bodyString := string(bodyBytes)
		log.Print("Schedules deleted, response: " + bodyString)
	} else {
		return errors.New("status code != 200")
	}
	return nil
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func today() time.Time {
	return truncateToDay(time.Now())
}

func tomorrow() time.Time {
	return today().AddDate(0, 0, 1)
}

func ParseDate(datestr string) (time.Time, error) {
	if datestr == "today" {
		return today(), nil
	} else if datestr == "tomorrow" {
		return tomorrow(), nil
	} else {
		return time.Time{}, errors.New("unknown date format: " + datestr)

	}
}

type TimeOffset struct {
	begin, end time.Duration
}

func ParseTime(hourstr string) (TimeOffset, error) {
	hours, err := ParseInts(hourstr, "..")
	if err != nil {
		return TimeOffset{}, err
	}
	if len(hours) != 2 {
		return TimeOffset{}, errors.New("incorrect time format: <hour_start>..<hour_end>")
	}
	s1 := time.Hour * time.Duration(hours[0])
	s2 := time.Hour * time.Duration(hours[1])
	return TimeOffset{s1, s2}, nil
}

type Params struct {
	Id int  `json:"id"`
	On bool `json:"on"`
}

type Call struct {
	Method string `json:"method"`
	Params Params `json:"params"`
}

type Schedule struct {
	Enable   bool   `json:"enable"`
	TimeSpec string `json:"timespec"`
	Calls    []Call `json:"calls"`
}

func getTimeSpec(t time.Time) string {
	weekdays := []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
	return fmt.Sprintf("%d %d %d %d %d %s", t.Second(), t.Minute(), t.Hour(),
		t.Day(), t.Month(), weekdays[int(t.Weekday())])
}

func createSchedulePayload(rid int, t time.Time, status bool) ([]byte, error) {
	params := Params{rid, status}
	call := Call{"Switch.Set", params}
	calls := []Call{call}
	schedule := Schedule{true, getTimeSpec(t), calls}
	return json.Marshal(schedule)
}

func sendSchedulePayload(uri string, payload []byte) error {
	resp, err := http.Post(uri+"Schedule.Create", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		bodyString := string(bodyBytes)
		log.Print("Schedule created, response: " + bodyString)
	} else {
		return errors.New("status code != 200")
	}
	return nil
}

func onoff() int {
	if len(os.Args) < 5 {
		usage_onoff()
		os.Exit(1)
	}
	relay_ids, err := ParseInts(os.Args[2], ",")
	if err != nil {
		log.Fatal(err)
	}
	ip, ok := os.LookupEnv("SHELLY_IP")
	if !ok {
		log.Fatal("Environment variable SHELLY_IP not set")
	}
	uri := "http://" + ip + "/rpc/"

	date, err := ParseDate(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}
	extraInfo := ""
	if date == today() {
		extraInfo += " (today)"
	}
	if date == tomorrow() {
		extraInfo += " (tomorrow)"
	}
	log.Printf("Settings relays for date " + date.Format("2006-01-02") + extraInfo)
	timeOffset, err := ParseTime(os.Args[4])
	if err != nil {
		log.Fatal(err)
	}

	err = CheckConnection(uri)
	if err != nil {
		log.Fatal(err)
	}

	err = ScheduleDeleteAll(uri)
	if err != nil {
		log.Fatal(err)
	}

	for i, rid := range relay_ids {
		offset := time.Second * time.Duration(2*i)
		d1 := date.Add(timeOffset.begin + offset)
		d2 := date.Add(timeOffset.end + offset)
		f1 := d1.Format("15:04:05")
		f2 := d2.Format("15:04:05")
		if (date.Format("2006-01-02") != d1.Format("2006-01-02")) ||
			(date.Format("2006-01-02") != d2.Format("2006-01-02")) {
			f1 = d1.Format("2006-01-02 15:04:05")
			f2 = d2.Format("2006-01-02 15:04:05")
		}

		log.Printf("Settings relay %d on between: %s ... %s\n", rid, f1, f2)
		payload, err := createSchedulePayload(rid, d1, true)
		if err != nil {
			log.Fatal(err)
		}
		log.Print("Payload for turn relay on: " + string(payload))
		err = sendSchedulePayload(uri, payload)
		if err != nil {
			log.Fatal(err)
		}
		payload, err = createSchedulePayload(rid, d2, false)
		if err != nil {
			log.Fatal(err)
		}
		log.Print("Payload for turn relay off: " + string(payload))
		err = sendSchedulePayload(uri, payload)
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Println("Everything done!")
	return 0
}

func usage() {
	fmt.Printf("Usage: %s <command> [<args>]\n\n", appName)
	fmt.Println("Command to easily turn relays on and off:")
	fmt.Println("  onoff      turn relay of list of relays on and off at certain time")
	fmt.Print("\nExamples:\n\n")
	fmt.Printf("  %s onoff 0,1,2 today 17..18\n", appName)
	fmt.Printf("  %s onoff 0 tomorrow 2..3\n", appName)
	fmt.Print("\n\n")
	fmt.Println("Note 1: by default, all earlier schedules are deleted before settings new ones.")
	fmt.Println("Note 2: an offset to time is set according to formula <relay_id>*10 seconds.")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	if os.Args[1] == "onoff" {
		os.Exit(onoff())
	} else {
		usage()
		os.Exit(1)
	}
}
