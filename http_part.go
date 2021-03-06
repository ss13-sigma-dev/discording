package main

import (
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/grokify/html-strip-tags-go"
	"html"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	port                 string
	admin_retrieval_page string
	client http.Client //for http requests timeouts
)

func index_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "It works despite not looking so")
}

func safe_param(m *url.Values, param string) string {
	if len((*m)[param]) < 1 {
		return ""
	}
	return (*m)[param][0]
}

type universal_parse struct {
	Ckey         string
	Message      string
	Token        string
	Status       string
	Reason       string
	Seclevel     string
	Event        string
	Data         string
	Keyname      string
	Role         string
	Round        string
	Add_num      int
	Has_follower int
}

func webhook_handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "" && r.Method != "GET" {
		return
	} //no POST and other shit
	defer logging_recover("WH")
	r.ParseForm()
	form := &r.Form
	key := safe_param(form, "key")
	var servername string
	for srvname, srv := range known_servers {
		if srv.webhook_key == key {
			servername = srvname
			break
		}
	}
	if servername == "" {
		fmt.Fprint(w, "No command handling without password")
		return
	}
	json_data := []byte(unfuck_byond_json(safe_param(form, "data"))) //Bquery_deconvert(safe_param(form, "data")))
	defer rise_error(servername)
	defer rise_error(string(json_data))
	var parsed universal_parse
	err := json.Unmarshal(json_data, &parsed)
	if err != nil {
		panic(fmt.Sprintf("%v (`'%v'` <- `'%v'`)", err, unfuck_byond_json(safe_param(form, "data")), safe_param(form, "data")))
	}
	switch safe_param(form, "method") {
	case "oocmessage":
		Discord_message_send(servername, "ooc", "OOC: "+parsed.Ckey, html.UnescapeString(parsed.Message))
	case "asaymessage":
		Discord_message_send(servername, "admin", "ASAY: "+parsed.Ckey, html.UnescapeString(parsed.Message))
	case "ahelpmessage":
		if parsed.Ckey != "" && strings.Index(parsed.Ckey, "->") == -1 { //because ADMINPM is AHELP too for some wicked reason
			last_ahelp[servername] = parsed.Ckey
		}
		Discord_message_send(servername, "admin", "AHELP: "+parsed.Ckey, html.UnescapeString(parsed.Message))
	case "memessage":
		if parsed.Message == "" {
			return //probably got hit by stunbaton, idk why it sends it
		}
		Discord_message_send(servername, "me", "EMOTE: "+parsed.Ckey, html.UnescapeString(parsed.Message))
	case "garbage":
		Discord_message_send(servername, "garbage", parsed.Ckey, strip.StripTags(html.UnescapeString(parsed.Message)))
	case "token":
		Discord_process_token(html.UnescapeString(parsed.Token), parsed.Ckey)
	case "runtimemessage":
		Discord_message_send(servername, "debug", "DEBUG", html.UnescapeString(parsed.Message))
	case "roundstatus":
		color := known_servers[servername].color
		embed := &discordgo.MessageEmbed{
			Color:  color,
			Fields: []*discordgo.MessageEmbedField{},
		}
		ss, ok := server_statuses[servername]
		ss_glob_update := func() {
			if ok {
				ss.global_update()
			}
		}
		switch parsed.Status {
		case "lobby":
			Discord_subsriber_message_send(servername, "bot_status", "New round is about to start (lobby)")
			ss_glob_update()

		case "ingame":
			Discord_subsriber_message_send(servername, "bot_status", "New round had just started")
			ss_glob_update()

		case "shuttle called":
			embed.Fields = []*discordgo.MessageEmbedField{&discordgo.MessageEmbedField{Name: "Code:", Value: parsed.Seclevel, Inline: true}, &discordgo.MessageEmbedField{Name: "Reason:", Value: Dsanitize(parsed.Reason), Inline: true}}
			embed.Title = "SHUTTLE CALLED"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			ss_glob_update()

		case "shuttle recalled":
			embed.Title = "SHUTTLE RECALLED"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			ss_glob_update()

		case "shuttle autocalled":
			embed.Title = "SHUTTLE AUTOCALLED"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			ss_glob_update()

		case "shuttle docked":
			embed.Title = "SHUTTLE DOCKED WITH THE STATION"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			ss_glob_update()

		case "shuttle left":
			embed.Title = "SHUTTLE LEFT THE STATION"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			ss_glob_update()

		case "shuttle escaped":
			embed.Title = "SHUTTLE DOCKED WITH CENTCOMM"
			Discord_send_embed(servername, "bot_status", embed)
			Discord_send_embed(servername, "ooc", embed)
			Discord_subsriber_message_send(servername, "bot_status", "Current round is about to end (roundend)")
			ss_glob_update()

		case "reboot":
			Discord_message_send_raw(servername, "ooc", "**===REBOOT===**")

		}
	case "status_update":
		ss, ok := server_statuses[servername]
		if !ok {
			return
		}
		switch parsed.Event {
		case "client_login", "client_logoff":
			ss.global_update()

		default:
			log.Print(form)
		}
	case "data_request":
		if parsed.Data == "shitspawn_list" {
			round, err := strconv.Atoi(parsed.Round)
			noerror(err)
			str := check_donators(servername, round)
			fmt.Fprint(w, str)
			log_line("shitspawn list -> "+str, "shitspawn_debug")
		}
	case "rolespawn":
		round, err := strconv.Atoi(parsed.Round)
		noerror(err)
		expend_donator(servername, parsed.Keyname, round, parsed.Role, parsed.Add_num, parsed.Has_follower > 0)
		log_line(fmt.Sprintf("shitspawn role -> %v %v %v %v %v %v", servername, parsed.Keyname, parsed.Round, parsed.Role, parsed.Add_num, parsed.Has_follower), "shitspawn_debug")
	default:
		log.Print(form)
	}
}

func init() {
	port = os.Getenv("PORT")
	if port == "" {
		log.Fatalln("Failed to retrieve $PORT")
	}
	timeout := time.Duration(5 * time.Second)
	client = http.Client{
	    Timeout: timeout,
	}
}

func Load_admins() {
	for s := range known_servers {
		Load_admins_for_server(s)
	}
}

type admin_data struct {
	Ckey string
	Rank string
}

func Load_admins_for_server(server string) {
	defer logging_recover("ADM " + server)
	servstruct, ok := known_servers[server]
	if !ok {
		panic("can't find server")
	}
	if(servstruct.admins_page=="") {
		adminssl := make([]string, 0)
		Known_admins[server] = adminssl
		log.Println("no admins for "+server)
		return
	}
	response, err := client.Get(servstruct.admins_page)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	bodyraw := string(body)

	admins_zlo := make(map[string][]string)
	admins_dobro := make([]admin_data, 0)
	if err := json.Unmarshal([]byte(bodyraw), &admins_zlo); err != nil {
		if err2 := json.Unmarshal([]byte(bodyraw), &admins_dobro); err2 != nil {
			panic(fmt.Sprintf("Both unmarshals failed:\n\t`%v`\n\t\t`%v`", err, err2))
		}
	}
	adminssl := make([]string, 0)
	for k, v := range admins_zlo {
		if k == "Removed" {
			continue
		}
		adminssl = append(adminssl, v...)
	}
	for _, v := range admins_dobro {
		if v.Rank == "Removed" {
			continue
		}
		adminssl = append(adminssl, v.Ckey)
	}
	Known_admins[server] = adminssl
	log.Println(adminssl)
}

var http_server_stop chan int

func Http_server() *http.Server {
	srv := &http.Server{Addr: ":" + port}
	http.HandleFunc("/", index_handler)
	http.HandleFunc("/command", webhook_handler)
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			log.Print("Http server error: ", err)
		}
	}()
	http_server_ticker := time.NewTicker(30 * time.Minute)
	go func() {
		for {
			select {
			case <-http_server_stop:
				http_server_ticker.Stop()
				return
			case <-http_server_ticker.C:
				_, _ = client.Get("http://discording312.herokuapp.com")
			}
		}
	}()
	return srv
}
