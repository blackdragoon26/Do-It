package main

import (
	"fmt"
	"net/http"
)

var shortgo = "watch go crash course"
var lonGo = "watch nana's golang full course"
var reward = "monster energy drnik"
var taskItems = []string{shortgo, reward, lonGo}

func main() {
	fmt.Println("##### Welcome to our ToDolist App####")
	http.HandleFunc("/", hell)
	http.HandleFunc("/show-tasks", showTasks)
	http.ListenAndServe(":8080", nil)

}

func hell(writer http.ResponseWriter, request *http.Request) {
	var greeting = "Hello, User\n Welcome to our ToDolist App"
	fmt.Fprintln(writer, greeting)
}
func showTasks(writer http.ResponseWriter, request *http.Request) {
	for _, x := range taskItems {
		fmt.Fprintln(writer, x)
	}
}
