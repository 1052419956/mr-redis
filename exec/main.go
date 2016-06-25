package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"

	exec "github.com/mesos/mesos-go/executor"
	mesos "github.com/mesos/mesos-go/mesosproto"

	typ "github.com/mesos/mr-redis/common/types"
	"github.com/mesos/mr-redis/exec/RedMon"
)

var DbType = flag.String("DbType", "etcd", "Type of the database etcd/zookeeper etc.,")
var DbEndPoint = flag.String("DbEndPoint", "", "Endpoint of the database")
var MrRedisLogger *log.Logger

type MrRedisExecutor struct {
	tasksLaunched int
	HostIP        string
	monMap        map[string](*RedMon.RedMon)
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func NewMrRedisExecutor() *MrRedisExecutor {
	return &MrRedisExecutor{tasksLaunched: 0}
}

func (exec *MrRedisExecutor) Registered(driver exec.ExecutorDriver, execInfo *mesos.ExecutorInfo, fwinfo *mesos.FrameworkInfo, slaveInfo *mesos.SlaveInfo) {
	fmt.Println("Registered Executor on slave ") //, slaveInfo.GetHostname())
}

func (exec *MrRedisExecutor) Reregistered(driver exec.ExecutorDriver, slaveInfo *mesos.SlaveInfo) {
	fmt.Println("Re-registered Executor on slave ") //, slaveInfo.GetHostname())
}

func (exec *MrRedisExecutor) Disconnected(exec.ExecutorDriver) {
	fmt.Println("Executor disconnected.")
}

func (exec *MrRedisExecutor) LaunchTask(driver exec.ExecutorDriver, taskInfo *mesos.TaskInfo) {
	fmt.Println("Launching task", taskInfo.GetName(), "with command", taskInfo.Command.GetValue())

	var runStatus *mesos.TaskStatus
	exec.tasksLaunched++
	M := RedMon.NewRedMon(taskInfo.GetTaskId().GetValue(), exec.HostIP, exec.tasksLaunched+6379, string(taskInfo.Data), MrRedisLogger)

	fmt.Printf("The Redmon object = %v\n", *M)

	tid := taskInfo.GetTaskId().GetValue()
	exec.monMap[tid] = M

	go func() {
		if M.Start() {
			runStatus = &mesos.TaskStatus{
				TaskId: taskInfo.GetTaskId(),
				State:  mesos.TaskState_TASK_RUNNING.Enum(),
			}
		} else {
			runStatus = &mesos.TaskStatus{
				TaskId: taskInfo.GetTaskId(),
				State:  mesos.TaskState_TASK_ERROR.Enum(),
			}
		}
		_, err := driver.SendStatusUpdate(runStatus)
		if err != nil {
			fmt.Println("Got error", err)
		}

		fmt.Println("Total tasks launched ", exec.tasksLaunched)

		//our server is now running, lets start monitoring it also
		go func() {
			M.Monitor()
		}()

		exit_state := mesos.TaskState_TASK_FINISHED.Enum()

		exit_err := M.Cmd.Wait() //TODO: Collect the return value of the process and send appropriate TaskUpdate eg:TaskFinished only on clean shutdown others will get TaskFailed
		if exit_err != nil || M.P.Msg != "SHUTDOWN" {
			//If the redis-server proc finished either with a non-zero or its not suppose to die then mark it as Task filed
			exit_state = mesos.TaskState_TASK_FAILED.Enum()
			//Signal the monitoring thread to stop monitoring from now on
			M.MonChan <- 1
		}

		// finish task
		fmt.Println("Finishing task", taskInfo.GetName())
		finStatus := &mesos.TaskStatus{
			TaskId: taskInfo.GetTaskId(),
			State:  exit_state,
		}
		_, err = driver.SendStatusUpdate(finStatus)
		if err != nil {
			fmt.Println("Got error", err)
		}
		fmt.Println("Task finished", taskInfo.GetName())
	}()
}

func (exec *MrRedisExecutor) KillTask(driver exec.ExecutorDriver, taskID *mesos.TaskID) {
	tid := taskID.GetValue()
	//tbd: is there any error check needed
	exec.monMap[tid].Die()

	fmt.Println("Killed task with task id:", tid)
}

func (exec *MrRedisExecutor) FrameworkMessage(driver exec.ExecutorDriver, msg string) {
	fmt.Println("Got framework message: ", msg)
}

func (exec *MrRedisExecutor) Shutdown(exec.ExecutorDriver) {
	fmt.Println("Shutting down the executor")
}

func (exec *MrRedisExecutor) Error(driver exec.ExecutorDriver, err string) {
	fmt.Println("Got error message:", err)
}

// -------------------------- func inits () ----------------- //
func init() {
	flag.Parse()
}

func main() {
	fmt.Println("Starting MrRedis Executor")

	typ.Initialize(*DbType, *DbEndPoint)

	var out io.Writer = ioutil.Discard

	out, _ = os.Create("/tmp/MrRedisExecutor.log")
	//ToDo does this need error handling
	MrRedisLogger = log.New(out, "[Info]", log.Lshortfile)

	MrRedisExec := NewMrRedisExecutor()
	MrRedisExec.HostIP = GetLocalIP()
	MrRedisExec.monMap = make(map[string](*RedMon.RedMon))

	dconfig := exec.DriverConfig{
		Executor: MrRedisExec,
	}
	driver, err := exec.NewMesosExecutorDriver(dconfig)

	if err != nil {
		fmt.Println("Unable to create a ExecutorDriver ", err.Error())
	}

	_, err = driver.Start()
	if err != nil {
		fmt.Println("Got error:", err)
		return
	}
	fmt.Println("Executor process has started and running.")
	_, err = driver.Join()
	if err != nil {
		fmt.Println("driver failed:", err)
	}
	fmt.Println("executor terminating")
}
