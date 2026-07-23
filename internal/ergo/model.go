package ergo

import "time"

type State string

const (
	StateTodo     State = "todo"
	StateDoing    State = "doing"
	StateBlocked  State = "blocked"
	StateError    State = "error"
	StateDone     State = "done"
	StateCanceled State = "canceled"
)

type Result struct {
	Summary           string
	Path              string
	SHA256AtAttach    string
	MtimeAtAttach     string
	GitCommitAtAttach string
	CreatedAt         time.Time
}

type Message struct {
	Kind      string
	Text      string
	CreatedAt time.Time
}

type Task struct {
	ID           string
	UUID         string
	ParentID     string
	Title        string
	Body         string
	State        State
	ClaimedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Container    bool
	Complete     bool
	Ready        bool
	Waiting      bool
	Dependencies []string
	Dependents   []string
	Children     []string
	Results      []Result
	Messages     []Message
}

type Summary struct {
	Containers int
	Ready      int
	Waiting    int
	Doing      int
	Blocked    int
	Error      int
	Done       int
	Canceled   int
	Total      int
}

type Snapshot struct {
	Root      string
	ErgoDir   string
	EventPath string
	Version   string
	Roots     []string
	Summary   Summary

	tasks []Task
	byID  map[string]int
}

func (s Snapshot) AllTasks() []Task {
	tasks := make([]Task, len(s.tasks))
	for index, task := range s.tasks {
		tasks[index] = cloneTask(task)
	}
	return tasks
}

func (s Snapshot) Task(id string) (Task, bool) {
	index, ok := s.byID[id]
	if !ok || index < 0 || index >= len(s.tasks) {
		return Task{}, false
	}
	return cloneTask(s.tasks[index]), true
}

func (s Snapshot) ChildrenOf(id string) []Task {
	task, ok := s.Task(id)
	if !ok {
		return nil
	}
	children := make([]Task, 0, len(task.Children))
	for _, childID := range task.Children {
		if child, exists := s.Task(childID); exists {
			children = append(children, child)
		}
	}
	return children
}

func cloneTask(task Task) Task {
	task.Dependencies = append([]string(nil), task.Dependencies...)
	task.Dependents = append([]string(nil), task.Dependents...)
	task.Children = append([]string(nil), task.Children...)
	task.Results = append([]Result(nil), task.Results...)
	task.Messages = append([]Message(nil), task.Messages...)
	return task
}
