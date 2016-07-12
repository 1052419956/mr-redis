package types

import (
	"container/list"
	"log"

	"github.com/mesos/mr-redis/common/store/etcd"
)

func Initialize(dbtype string, config string) (bool, error) {

	//Initalize all the communication channels
	OfferList = list.New()
	OfferList.Init()
	Cchan = make(chan TaskCreate)
	Mchan = make(chan *TaskUpdate) //Channel for Maintainer
	Dchan = make(chan TaskMsg)     //Channel for Destroyer

	//Initalize the Internal in-memory storage
	MemDb = NewInMem()

	//Initalize the store db
	switch dbtype {
	case "etcd":
		Gdb = etcd.New()
		err := Gdb.Setup(config)
		if err != nil {
			log.Fatalf("Failed to setup etcd database error:%v", err)
		}
		return Gdb.IsSetup(), nil
	}

	return true, nil
}
