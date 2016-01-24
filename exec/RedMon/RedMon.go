package RedMon

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	redisclient "gopkg.in/redis.v3"
	typ "../../common/types"
)

//This structure is used to implement a monitor thread/goroutine for a running Proc(redisProc)
//This structure should be extended only if more functionality is required on the Monitoring functionality
//A Redis Proc's objec is created within this and monitored hence forth
type RedMon struct {
	P       *typ.Proc //The Proc structure that should be used
	Pid     int       //The Pid of the running proc
	IP      string    //IP address the redis instance should bind to
	Port    int       //The port number of this redis instance to be started
	Ofile   io.Writer //Stdout log file to be re-directed to this io.writer
	Efile   io.Writer //stderr of the redis instnace should be re-directed to this file
	MS_Sync bool      //Make this as master after sync
	monChan chan int
	Cmd     *exec.Cmd
	Client  *redisclient.Client	//redis client library connection handler
	//cgroup *CgroupManager		//Cgroup manager/cgroup connection pointer
}

//Create a new monitor based on the Data sent along with the TaskInfo
//The data could have the following details
//Capacity Master                 => Just start this PROC send update as TASK_RUNNING and monitor henceforth
//Capacity SlaveOf IP:Port        => This is a redis slave so start it as a slave, sync and then send TASK_RUNNING update then Monitor
//Capacity Master-SlaveOf IP:Port => This is a New master of the instance with an upgraded memory value so
//                          Start as slave, Sync data, make it as master, send TASK_RUNNING update and start to Monitor
func NewRedMon(tskName string, IP string, Port int, data string) *RedMon {

	var R RedMon
	var P *typ.Proc

	R.monChan = make(chan int)
	R.Port = Port
	R.IP = IP
	split_data := strings.Split(data, " ")

	fmt.Printf("Split data recived is %v\n", data)
	if len(split_data) < 1 || len(split_data) > 4 {
		//Print an error this is not suppose to happen
		fmt.Printf("RedMon Splitdata error %v\n", split_data)
		return nil
	}

	Cap, _ := strconv.Atoi(split_data[0])

	switch split_data[1] {
	case "Master":
		P = typ.NewProc(tskName, Cap, "M", "")
		break
	case "SlaveOf":
		P = typ.NewProc(tskName, Cap, "S", split_data[2])
		break
	case "Master-SlaveOf":
		P = typ.NewProc(tskName, Cap, "MS", split_data[2])
		R.MS_Sync = true
		break
	}
	R.P = P
	//ToDo each instance should be started with its own dir and specified config file
	//ToDo Stdout file to be tskname.stdout
	//ToDo stderere file to be tskname.stderr

	return &R
}

//Start the redis Proc be it Master or Slave
func (R *RedMon) Start() bool {

	if R.P.SlaveOf == "" {
		return R.StartMaster()
	} else {

		if !R.MS_Sync {
			return R.StartSlave()
		} else {
			//Posibly a scale request so start it as a slave, sync then make as master
			return R.StartSlaveAndMakeMaster()
		}
	}

	return false
}

func (R *RedMon) StartMaster() bool {
	//Command Line
	R.Cmd = exec.Command("redis-server", "--port", fmt.Sprintf("%d", R.Port))
	err := R.Cmd.Start()

	if err != nil {
		//Print some error
		return false
	}

	R.Pid = R.Cmd.Process.Pid
	R.P.Pid = R.Cmd.Process.Pid
	R.P.Port = fmt.Sprintf("%d", R.Port)
	R.P.IP = R.IP
	R.P.State = "Running"
	R.P.Sync()

	return true
}

func (R *RedMon) StartSlave() bool {
	//Command Line
	slaveof := strings.Split(R.P.SlaveOf, ":")
	if len(slaveof) != 2 {
		log.Printf("Unacceptable SlaveOf value %vn", R.P.SlaveOf)
		return false
	}
	R.Cmd = exec.Command("redis-server", "--port", fmt.Sprintf("%d", R.Port), "--SlaveOf", slaveof[0], slaveof[1])
	err := R.Cmd.Start()

	if err != nil {
		//Print some error
		return false
	}

	//Monitor the redis PROC to check if the sync is complete
	for !R.IsSyncComplete() {
		time.Sleep(time.Second)
	}
	R.Pid = R.Cmd.Process.Pid
	R.P.Pid = R.Cmd.Process.Pid
	R.P.Port = fmt.Sprintf("%d", R.Port)
	R.P.IP = R.IP
	R.P.State = "Running"

	R.P.Sync()

	return true
}

func (R *RedMon) StartSlaveAndMakeMaster() bool {
	//Command Line
	slaveof := strings.Split(R.P.SlaveOf, ":")
	if len(slaveof) != 2 {
		fmt.Printf("Unacceptable SlaveOf value %vn", R.P.SlaveOf)
		return false
	}
	R.Cmd = exec.Command("redis-server", "--port", fmt.Sprintf("%d", R.Port), "--SlaveOf", slaveof[0], slaveof[1])
	err := R.Cmd.Start()

	if err != nil {
		//Print some error
		return false
	}

	R.Pid = R.Cmd.Process.Pid

	//Monitor the redis PROC to check if the sync is complete
	for !R.IsSyncComplete() {
		time.Sleep(time.Second)
	}
	//Make this Proc as master
	R.MakeMaster()

	R.Pid = R.Cmd.Process.Pid
	R.P.Pid = R.Cmd.Process.Pid
	R.P.Port = fmt.Sprintf("%d", R.Port)
	R.P.IP = R.IP
	R.P.State = "Running"
	R.P.Sync()

	return true
}

func (R *RedMon) GetConnectedClient() *redisclient.Client {

	log.Printf("Monitoring stats")

	client := redisclient.NewClient(&redisclient.Options{
		Addr:     R.IP + fmt.Sprintf("%d", R.Port),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	pong, err := client.Ping().Result()
	log.Printf(pong, err)

	return client
}

func (R *RedMon) UpdateStats() bool {

	var redisStats typ.Stats
	var err error

	redisStats.Mem, err = R.Client.Info("memory").Result()
	if err != nil {
		log.Printf("STATS collection returned error on IP:%s and PORT:%d Err:%v", R.IP, R.Port, err)
		return false
	}

	redisStats.Cpu, err = R.Client.Info("cpu").Result()
	if err != nil {
		log.Printf("STATS collection returned error on IP:%s and PORT:%d Err:%v", R.IP, R.Port, err)
		return false
	}

	redisStats.Others, err = R.Client.Info("stats").Result()
	if err != nil {
		log.Printf("STATS collection returned error on IP:%s and PORT:%d Err:%v", R.IP, R.Port, err)
		return false
	}

	R.P.Stats = R.P.ToJsonStats(redisStats)

	errSync := R.P.SyncStats()
	if !errSync{
		log.Printf("Error syncing stats to store")
		return false
	}
	return true
}


func (R *RedMon) Monitor() bool {


   	//wait for a second for the server to start
	//ToDo: is it needed
	time.Sleep(1 * time.Second)
	//then initiate a connection to it; for stats
	R.Client = R.GetConnectedClient()

	for {
		select {

		case <-R.monChan:
		//ToDo:update state if needed
		//signal to stop monitoring this
			return false

		case <-time.After(100 * time.Millisecond):
			R.CheckMsg()

		case <-time.After(1 * time.Second):
			R.UpdateStats()
		}

	}

}

func (R *RedMon) Stop() bool {


   //send SHUTDOWN command for a gracefull exit of the redis-server
	//the server exited gracefully will reflect at the task status FINISHED
	err := R.Client.Shutdown()
	if err != nil{
		log.Printf("problem shutting down the server at IP:%s and port:%d with error %v", R.IP, R.Port, err)
		//in the error case also we need to tell scheduler that graceful shutdown did not happen
		//scheduler can send a kill on executor with the task ID
		R.P.Msg = "SHUTDOWN_ERROR"
		errMsg := R.P.SyncMsg()
		if !errMsg{//message should be read by scheduler
			log.Printf("could not update the message to scheduler SHUTDOWN_ERROR FOR IP:%s and port:%d", R.IP, R.Port)
		}
		return false
	}

	//in case of a successful shutdown we should stop monitoring also
	//the redis client will anyways return error
	R.monChan <-1
	return true

}

func (R *RedMon) Die() bool {
	//err := nil
	err := R.Cmd.Process.Kill()
	if err != nil {
		log.Printf("Unable to kill the process %v", err)
		return false
	}

 	return true
}

func (R *RedMon) CheckMsg() {
	//check message from scheduler
	//currently we do it to see if scheduler asks us to quit

	//better error handling needed
	err := R.P.LoadMsg()
	if err{

		log.Printf("failed to load the MSG from store")
		return
	}

	if "SHUTDOWN" == R.P.Msg{
		err = R.Stop()
		if err{

			log.Printf("failed to stop the REDIS server")
		}
	}

}


//Should be called by the Monitors on Slave Procs, this gives the boolien anser if the sync is complegted or not
func (R *RedMon) IsSyncComplete() bool {

	//Keep checking if the sync of data freom master is completed or not
	return true
}

func (R *RedMon) MakeMaster() bool {

	//Send a cli config comamnd to make a current Proc a master from a slave
	return true
}
