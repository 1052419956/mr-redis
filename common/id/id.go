package id

import (
	"github.com/nu7hatch/gouuid"
)

func NewUUID() (*uuid.UUID, error) {

	rUUID, err := uuid.NewV4()

	if err != nil {
		return nil, err
	}

	return rUUID, nil

}
