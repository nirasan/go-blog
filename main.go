package main

import (
	"net/http"
	"html/template"
	"github.com/gorilla/mux"
	"github.com/gorilla/context"
	"github.com/gorilla/sessions"
	"errors"
	"database/sql"
	"gopkg.in/gorp.v1"
	_ "github.com/mattn/go-sqlite3"
	"time"
	"log"
)

var (
	// セッションストアの初期化
	store *sessions.CookieStore = sessions.NewCookieStore([]byte("12345678901234567890"))
)

const (
	SessionName = "session-name"
	ContextSessionKey = "session"
	ContextDbmapKey = "dbmap"
)

func main() {
	r := mux.NewRouter()
	handleFunc(r, "/", rootHandler)
	handleFunc(r, "/register", registerGetHandler).Methods("GET")
	handleFunc(r, "/register", registerPostHandler).Methods("POST")
	handleFunc(r, "/login", loginGetHandler).Methods("GET")
	handleFunc(r, "/login", loginPostHandler).Methods("POST")
	handleFunc(r, "/logout", logoutGetHandler).Methods("GET")
	handleFunc(r, "/logout", logoutPostHandler).Methods("POST")
	handleFunc(r, "/entry", entryGetHandler).Methods("GET")
	handleFunc(r, "/entry", entryPostHandler).Methods("POST")
	http.ListenAndServe(":8080", r)
}

// アプリケーション共通処理を常に呼び出すための糖衣構文
func handleFunc(r *mux.Router, path string, fn http.HandlerFunc) *mux.Route {
	return r.HandleFunc(path, applicationHandler(fn))
}

// アプリケーション共通処理
func applicationHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// セッションの取得
		session, err := store.Get(r, SessionName)
		checkError(err)
		context.Set(r, ContextSessionKey, session)
		// DB初期化
		dbmap := initDb()
		defer dbmap.Db.Close()
		context.Set(r, ContextDbmapKey, dbmap)
		// 個別のハンドラー呼び出し
		fn(w, r)
	}
}

// トップページ
func rootHandler(w http.ResponseWriter, r *http.Request) {
	type page struct {
		Username string
	}
	p := &page{}
	username, err := getUsernameFromSession(r)
	log.Println(username)
	log.Println(err)
	if err == nil {
		p.Username = username
	}
	log.Println(p)
	executeTemplate(w, "template/index.html", p)
}

// 登録ページ
func registerGetHandler(w http.ResponseWriter, r *http.Request) {
	executeTemplate(w, "template/register.html", nil)
}

// 登録処理
func registerPostHandler(w http.ResponseWriter, r *http.Request) {
	// フォームのパース
	r.ParseForm()
	// フォームの値の取得
	username, password := r.Form["username"][0], r.Form["password"][0]
	// 空なら登録ページへ
	if username == "" || password == "" {
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	// ユーザー登録
	user := NewUser(username, password)
	dbmap, err := getDb(r)
	checkError(err)
	err = dbmap.Insert(&user)
	checkError(err)
	// 現在のユーザーをセッションで管理
	session, err := getSession(r)
	checkError(err)
	session.Values["username"] = username
	session.Save(r, w)
	// トップページへ
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ログインページ
func loginGetHandler(w http.ResponseWriter, r *http.Request) {
	executeTemplate(w, "template/login.html", nil)
}

// ログイン処理
func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	// フォームのパース
	r.ParseForm()
	// フォームの値の取得
	username, password := r.Form["username"][0], r.Form["password"][0]
	// 空ならログイン画面へ
	if username == "" && password == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	// ユーザーの取得
	dbmap, err := getDb(r)
	checkError(err)
	user := User{}
	err = dbmap.SelectOne(&user, "SELECT * FROM users WHERE Name = ? AND Password = ?", username, password)
	// 存在しなければエラー
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	// 存在したらセッションに入れてログイン状態に
	session, err := getSession(r)
	checkError(err)
	session.Values["username"] = username
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ログアウトページ
func logoutGetHandler(w http.ResponseWriter, r *http.Request) {
	executeTemplate(w, "template/logout.html", nil)
}

// ログアウト処理
func logoutPostHandler(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r)
	checkError(err)
	delete(session.Values, "username")
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// エントリー一覧
func entryGetHandler(w http.ResponseWriter, r *http.Request) {
	// ユーザー名取得
	username, err := getUsernameFromSession(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	// ユーザー取得
	dbmap, err := getDb(r)
	checkError(err)
	var user User
	err = dbmap.SelectOne(&user, "SELECT * FROM users WHERE name = ?", username)
	checkError(err)
	// エントリー一覧取得
	var entries []Entry
	_, err = dbmap.Select(&entries, "SELECT * FROM entries WHERE UserId = ?", user.Id)
	// テンプレート表示
	type page struct {
		Entries []Entry
	}
	executeTemplate(w, "template/entries.html", page{Entries: entries})
}

// エントリー投稿
func entryPostHandler(w http.ResponseWriter, r *http.Request) {
	// ユーザー名取得
	username, err := getUsernameFromSession(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	// ユーザー取得
	dbmap, err := getDb(r)
	checkError(err)
	var user User
	err = dbmap.SelectOne(&user, "SELECT * FROM users WHERE name = ?", username)
	checkError(err)
	// フォームのパース
	r.ParseForm()
	// フォームの値の取得
	title, body := r.Form["title"][0], r.Form["body"][0]
	// 空なら一覧画面へ
	if title == "" && body == "" {
		http.Redirect(w, r, "/entry", http.StatusFound)
		return
	}
	entry := NewEntry(user, title, body)
	err = dbmap.Insert(&entry)
	checkError(err)
	http.Redirect(w, r, "/entry", http.StatusSeeOther)
}

// セッションの取得
func getSession(r *http.Request) (*sessions.Session, error) {
	if v := context.Get(r, ContextSessionKey); v != nil {
		return v.(*sessions.Session), nil
	}
	return nil, errors.New("failed to get session")
}

// DBの取得
func getDb(r *http.Request) (*gorp.DbMap, error) {
	if v := context.Get(r, ContextDbmapKey); v != nil {
		return v.(*gorp.DbMap), nil
	}
	return nil, errors.New("failed to get dbmap")
}

// セッションからユーザー名の取得
func getUsernameFromSession(r *http.Request) (string, error) {
	session, err := getSession(r)
	checkError(err)
	if v, ok := session.Values["username"]; ok {
		return v.(string), nil
	} else {
		return "", errors.New("username not found")
	}
}

// テンプレートの実行
func executeTemplate(w http.ResponseWriter, name string, data interface{}) {
	t, err := template.ParseFiles(name)
	checkError(err)
	err = t.Execute(w, data)
	checkError(err)
}

// エラーチェック
func checkError(err error) {
	if err != nil {
		log.Panicln(err)
	}
}

// ユーザークラス
type User struct {
	Id int64
	Created int64
	Name string
	Password string
}

// エントリークラス
type Entry struct {
	Id int64
	Created int64
	UserId int64
	Title string
	Body string `db:",size:65535"`
}

func NewUser(name, password string) User {
	return User{
		Created: time.Now().UnixNano(),
		Name: name,
		Password: password,
	}
}

func NewEntry(user User, title, body string) Entry {
	return Entry{
		Created: time.Now().UnixNano(),
		UserId: user.Id,
		Title: title,
		Body: body,
	}
}

// DB初期化
func initDb() *gorp.DbMap {
	db, err := sql.Open("sqlite3", "/tmp/entry_db.bin")
	checkError(err)
	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}
	// テーブルと構造体のひもづけ
	dbmap.AddTableWithName(User{}, "users").SetKeys(true, "Id")
	dbmap.AddTableWithName(Entry{}, "entries").SetKeys(true, "Id")
	// テーブルがなければ作成
	err = dbmap.CreateTablesIfNotExists()
	checkError(err)
	return dbmap
}

