package main

import (
	//数据库驱动
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

//SQL 数据库结构体
type SQL struct {
	Addr     string `json:"mysql_addr"`
	Account  string `json:"mysql_account"`
	Password string `json:"mysql_password"`
	DBname   string `json:"mysql_dbname"`
	TBname   string `json:"mysql_tbname"`
	Conn     *sql.DB
}

//popinJSONObj 爬取的json数据结构
type popinJSONObj struct {
	TotalMoney string `json:"total_charge"`
}

//POP POPin的相关信息
type POP struct {
	Account      string   `json:"popin_account"`
	CampaignList []string `json:"popin_CampaignList"`
	Cookie       string   `json:"popin_cookie"`
}

//PopinDB 实例化数据库对象
var PopinDB SQL

//Popin 实例化一个POP对象
var Popin POP
var err error

func init() {
	var file *os.File
	var configByte []byte
	if file, err = os.Open("config.json"); err != nil {
		log.Fatal(err)
	}
	if configByte, err = ioutil.ReadAll(file); err != nil {
		log.Fatal(err)
	}
	if err = json.Unmarshal(configByte, &PopinDB); err != nil {
		log.Fatal(err)
	}
	if err = json.Unmarshal(configByte, &Popin); err != nil {
		log.Fatal(err)
	}
	PopinDB.Conn, err = sql.Open("mysql", PopinDB.Account+":"+PopinDB.Password+"@tcp("+PopinDB.Addr+")/"+PopinDB.DBname)
	if err != nil {
		log.Fatalln("数据库", PopinDB.DBname, "不存在：", err.Error())
		// panic(err.Error())
	} else {
		fmt.Printf("已连接至数据库：%s", PopinDB.Addr)
	}

	stmtIns, err := PopinDB.Conn.Query("CREATE TABLE IF NOT EXISTS `" + PopinDB.DBname + "`.`" + PopinDB.TBname + "` ( `id` INT NOT NULL AUTO_INCREMENT ,`total_charge` INT(64) NOT NULL , `today_charge` INT(64) NOT NULL, `date` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP , PRIMARY KEY (`id`)) ENGINE = InnoDB;")
	if err != nil {
		panic(err.Error())
	}
	defer stmtIns.Close()
}

func main() {
	for {
		//开始更新
		yestM := PopinDB.ReadYesterdayCharge()
		nowM := Popin.GetAllMoney()
		charge := nowM - yestM
		PopinDB.ReplaceTodayCharge(nowM, charge)
		fmt.Printf("yestM:%d,nowM:%d", yestM, nowM)
		<-time.NewTimer(time.Second * 300).C
	}
}

//ReplaceTodayCharge 更新当天数据，不存在则创建
func (t *SQL) ReplaceTodayCharge(totalCharge, todayCharge int) {
	datestr := GetInsertDate()
	sqlml := "REPLACE INTO " + t.TBname + " (id,total_charge,today_charge) VALUES('" + datestr + "','" + strconv.Itoa(totalCharge) + "', '" + strconv.Itoa(todayCharge) + "');"
	// fmt.Println(sqlml)
	stmtOut, err := t.Conn.Query(sqlml)
	if err != nil {
		fmt.Println(err)
	}
	defer stmtOut.Close()
}

//ReadYesterdayCharge 获取前一天的消耗
func (t *SQL) ReadYesterdayCharge() int {
	sqlml := "SELECT total_charge FROM `" + t.TBname + "` WHERE `id` = '" + GetYesterdaydate() + "' LIMIT 1"
	//fmt.Println(sqlml)
	rows, err := t.Conn.Query(sqlml)
	if err != nil {
		fmt.Println(err)
	}

	//defer outres.Close()
	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}

	// Make a slice for the values
	values := make([]sql.RawBytes, len(columns))

	// rows.Scan wants '[]interface{}' as an argument, so we must copy the
	// references into such a slice
	// See http://code.google.com/p/go-wiki/wiki/InterfaceSlice for details
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	var value string
	// Fetch rows
	for rows.Next() {
		// get RawBytes from data
		err = rows.Scan(scanArgs...)
		if err != nil {
			panic(err.Error()) // proper error handling instead of panic in your app
		}
		// Now do something with the data.
		// Here we just print each column as a string.

		for _, col := range values {
			// Here we can check if the value is nil (NULL value)
			if col == nil {
				value = "0"
			} else {
				value = string(col)
			}
			//fmt.Println(columns[i], ": ", value)
		}

	}
	if err = rows.Err(); err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}
	//fmt.Println(value)
	charge, _ := strconv.Atoi(value)
	return charge
}

//GetAllMoney 获取popin指定campaign的总消耗,最常用的
func (pop *POP) GetAllMoney() (money int) {
	for _, v := range pop.CampaignList {
		j := getPopinMoney(pop.Cookie, pop.Account, v)
		money += j
	}
	return
}

//GetPopinMoney 爬取所需要的数据（消耗额）,核心函数,不要手贱的去"优化"
func getPopinMoney(popcookie, account, campaign string) int {
	campaignurl := "https://dashboard.popin.cc/discovery/accounts-tw/index.php/" + account + "/c/" + campaign + "/getDiscoveryReports"
	referer := "https://dashboard.popin.cc/discovery/accounts-tw/index.php/" + account + "/manageAgency?subPage=/" + account + "/campaigns/listCampaign"

	client := &http.Client{}
	var resp *http.Response
	req, err := http.NewRequest("GET", campaignurl, nil)
	if err != nil {
		fmt.Println("http.NewRequest Failed:", err)
		return 0
	}

	req.Header.Add("Host", "dashboard.popin.cc")
	req.Header.Add("User-Agent", GetRandomUserAgent())
	req.Header.Add("Accept", "*/*")
	req.Header.Add("Accept-Language", "zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en-US;q=0.3,en;q=0.2")
	req.Header.Add("Accept-Encoding", "gzip, deflate, br")
	req.Header.Add("X-Requested-With", "XMLHttpRequest")
	req.Header.Add("Connection", "keep-alive")
	req.Header.Add("Referer", referer)
	req.Header.Add("Cookie", popcookie)
	req.Header.Add("Pragma", "no-cache")
	req.Header.Add("Cache-Control", "no-cache")

	//发送请求数据之服务器，并获取响应数据
	resp, err = client.Do(req)
	if err != nil {
		fmt.Println("Client Do Failed:", err)
		return 0
	}
	if resp.StatusCode != 200 {
		fmt.Println("status code error: ", resp.StatusCode, resp.Status)
		return 0
	}
	defer resp.Body.Close()

	//var body string
	//switch resp.Header.Get("Content-Encoding") {
	//case "gzip":
	//	reader, _ := gzip.NewReader(resp.Body)
	//	for {
	//		buf := make([]byte, 1024)
	//		n, err := reader.Read(buf)
	//
	//		if err != nil && err != io.EOF {
	//			panic(err)
	//		}
	//
	//		if n == 0 {
	//			break
	//		}
	//		body += string(buf)
	//	}
	//default:
	//	bodyByte, _ := ioutil.ReadAll(resp.Body)
	//	body = string(bodyByte)
	//}

	bodyByte, _ := ioutil.ReadAll(resp.Body)
	//开始处理信息
	var popJSON popinJSONObj
	//解码json数据，将字节切片映射到指定结构上
	e := json.Unmarshal(bodyByte, &popJSON)
	if e != nil {
		fmt.Println("json.Unmarshal failed")
		return 0
	}

	//fmt.Println(popJSON.TotalMoney)

	moneystring := strings.Replace(popJSON.TotalMoney, ",", "", -1)
	moneystring = moneystring[:len(moneystring)-3]

	moneyint, err := strconv.Atoi(moneystring)
	if err != nil {
		fmt.Println("strconv.Atoi failed:", err)
		return 0
	}

	return moneyint
}

//GetInsertDate 获取应该生成的时间日期
func GetInsertDate() string {
	timeStr := time.Now().Format("20060102")
	t, _ := time.Parse("20060102", timeStr)
	timeNumber := t.Unix()
	intervalNumber := time.Now().Unix() - timeNumber

	if intervalNumber > 5430 {
		//如果大于09:30，返回今天日期
		return timeStr
	}
	//如果小于09:30,返回昨天日期
	return GetYesterdaydate()

}

//GetYesterdaydate 常规---获取昨天日期
func GetYesterdaydate() string {
	now := time.Now()
	d, _ := time.ParseDuration("-24h")
	dres := now.Add(d)
	yesterday := dres.Format("20060102")
	return yesterday
}

//GetlastDate 逻辑---获取昨天日期
func GetlastDate() string {
	strdate := GetInsertDate()
	t, _ := time.Parse("20060102", strdate)
	d, _ := time.ParseDuration("-24h")
	dres := t.Add(d)
	yesterday := dres.Format("20060102")
	return yesterday
}

//GetRandomUserAgent 从切片中随机抽取一个user-agent
func GetRandomUserAgent() string {
	var userAgentList = []string{"Mozilla/5.0 (compatible, MSIE 10.0, Windows NT, DigExt)",
		"Mozilla/4.0 (compatible, MSIE 8.0, Windows NT 6.0, Trident/4.0)",
		"Mozilla/4.0 (compatible, MSIE 7.0, Windows NT 5.1, 360SE)",
		"Mozilla/5.0 (compatible, MSIE 9.0, Windows NT 6.1, Trident/5.0,",
		"Opera/9.80 (Windows NT 6.1, U, en) Presto/2.8.131 Version/11.11",
		"Mozilla/4.0 (compatible, MSIE 7.0, Windows NT 5.1, TencentTraveler 4.0)",
		"Mozilla/5.0 (Windows, U, Windows NT 6.1, en-us) AppleWebKit/534.50 (KHTML, like Gecko) Version/5.1 Safari/534.50",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.117 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:72.0) Gecko/20100101 Firefox/72.0",
		"Mozilla/5.0 (Macintosh, Intel Mac OS X 10_7_0) AppleWebKit/535.11 (KHTML, like Gecko) Chrome/17.0.963.56 Safari/535.11",
		"Mozilla/5.0 (Macintosh, U, Intel Mac OS X 10_6_8, en-us) AppleWebKit/534.50 (KHTML, like Gecko) Version/5.1 Safari/534.50",
		"Mozilla/5.0 (Linux, U, Android 3.0, en-us, Xoom Build/HRI39) AppleWebKit/534.13 (KHTML, like Gecko) Version/4.0 Safari/534.13",
		"Mozilla/5.0 (iPad, U, CPU OS 4_3_3 like Mac OS X, en-us) AppleWebKit/533.17.9 (KHTML, like Gecko) Version/5.0.2 Mobile/8J2 Safari/6533.18.5",
		"Mozilla/4.0 (compatible, MSIE 7.0, Windows NT 5.1, Trident/4.0, SE 2.X MetaSr 1.0, SE 2.X MetaSr 1.0, .NET CLR 2.0.50727, SE 2.X MetaSr 1.0)",
		"Mozilla/5.0 (iPhone, U, CPU iPhone OS 4_3_3 like Mac OS X, en-us) AppleWebKit/533.17.9 (KHTML, like Gecko) Version/5.0.2 Mobile/8J2 Safari/6533.18.5",
		"MQQBrowser/26 Mozilla/5.0 (Linux, U, Android 2.3.7, zh-cn, MB200 Build/GRJ22, CyanogenMod-7) AppleWebKit/533.1 (KHTML, like Gecko) Version/4.0 Mobile Safari/533.1"}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return userAgentList[r.Intn(len(userAgentList))]
}

//GzipDecode 将数据进行解压缩
func GzipDecode(zip []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(zip))
	if err != nil {
		var out []byte
		return out, err
	}
	defer reader.Close()
	return ioutil.ReadAll(reader)
}

// RandString 生成随机字符串
func RandString(len int) string {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	bytes := make([]byte, len)
	for i := 0; i < len; i++ {
		b := r.Intn(26) + 65 + 32
		bytes[i] = byte(b)
	}
	return string(bytes)
}
