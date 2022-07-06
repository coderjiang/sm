package common

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var Lang = message.NewPrinter(language.Chinese)

type AvailableTrigger struct {
	TranslatedTrigger string
	Trigger           string
}

type StateMachineLog struct {
	gorm.Model
	ObjectId     uint   `gorm:"not null; index"`
	ObjectStruct string `gorm:"not null; index; varchar(64)"`
	Trigger      string `gorm:"not null; varchar(64)"`
	Source       string `gorm:"not null; varchar(64)"`
	Dest         string `gorm:"not null; varchar(64)"`
	OperatorId   uint   `gorm:"not null; index"`
}

func StructName(obj interface{}) string {
	if t := reflect.TypeOf(obj); t.Kind() == reflect.Ptr {
		return t.Elem().Name()
	} else {
		return t.Name()
	}
}

type Stater interface {
	States() []string
	Triggers() map[string]map[string]interface{}
	GetState() string
	SetState(state string)
	SetStater(stater Stater)
}

type Transition struct {
	State string `gorm:"type:varchar(64);not null;default:INITIALIZED"`
}

func (ts *Transition) GetState() string {
	return ts.State
}

func (ts *Transition) SetState(state string) {
	ts.State = state
}

type StateMachine struct {
	stater Stater `gorm:"-"`
	Transition
}

func (sm *StateMachine) SetStater(stater Stater) {
	sm.stater = stater
}

func (sm *StateMachine) AfterFind(tx *gorm.DB) error {
	ele := reflect.ValueOf(tx.Statement.Model).Elem()
	switch ele.Kind() {
	case reflect.Slice:
		for i := 0; i < ele.Len(); i++ {
			val := ele.Index(i).Interface()
			obj := val.(Stater)
			obj.SetStater(obj)
		}
	case reflect.Struct:
		s := tx.Statement.Model.(Stater)
		s.SetStater(s)
	default:
		return errors.New("StateMachine AfterFind unknown type")
	}
	return nil
}

func (sm *StateMachine) TranslatedState() string {
	return Lang.Sprintf(StructName(sm.stater) + ":" + sm.stater.GetState())
}

func (sm *StateMachine) AvailableTriggers() (triggers []*AvailableTrigger) {
	for trigger, config := range sm.stater.Triggers() {
		source := config["source"]
		for _, src := range strings.Split(source.(string), ",") {
			if src == sm.stater.GetState() {
				triggers = append(triggers,
					&AvailableTrigger{
						TranslatedTrigger: Lang.Sprintf(StructName(sm.stater) + ":" + trigger),
						Trigger:           trigger,
					})
			}
		}
	}
	return triggers
}

func (sm *StateMachine) Do(tx *gorm.DB, trigger string, userInfoId uint, args ...interface{}) error {
	if _, ok := sm.stater.Triggers()[trigger]; !ok {
		return errors.New(fmt.Sprintf("can not do trigger: %s", trigger))
	}

	source := sm.stater.Triggers()[trigger]["source"].(string)
	dest := sm.stater.Triggers()[trigger]["dest"].(string)
	beforeFunc := sm.stater.Triggers()[trigger]["before"]
	afterFunc := sm.stater.Triggers()[trigger]["after"]
	conditionFunc := sm.stater.Triggers()[trigger]["condition"]

	currentState := sm.stater.GetState()

	canDo := false
	var src string
	for _, src = range strings.Split(source, ",") {
		if src == currentState {
			canDo = true
		}
	}

	if !canDo {
		return errors.New(fmt.Sprintf("can not do trigger: %s, current state: %s", trigger, currentState))
	}

	if conditionFunc != nil {
		if !conditionFunc.(func(*gorm.DB, ...interface{}) bool)(tx, args...) {
			return nil
		}
	}

	if beforeFunc != nil {
		if err := beforeFunc.(func(*gorm.DB, ...interface{}) error)(tx, args...); err != nil {
			return err
		}
	}

	sm.stater.SetState(dest)

	if err := tx.Debug().Model(
		sm.stater,
	).Omit(clause.Associations).Update(
		"state", dest,
	).Error; err != nil {
		return err
	}

	if afterFunc != nil {
		if err := afterFunc.(func(*gorm.DB, ...interface{}) error)(tx, args...); err != nil {
			return err
		}
	}
	fmt.Println(tx, src, dest)

	return sm.log(tx, trigger, src, dest, userInfoId)
}

func (sm *StateMachine) log(tx *gorm.DB, trigger, source, dest string, userInfoId uint) error {
	if err := tx.Create(&StateMachineLog{
		ObjectId:     uint(reflect.ValueOf(sm.stater).Elem().FieldByName("ID").Uint()),
		ObjectStruct: StructName(sm.stater),
		Trigger:      trigger,
		Source:       source,
		Dest:         dest,
		OperatorId:   userInfoId,
	}).Error; err != nil {
		return err
	}
	return nil
}

func AutoMigrateStateStateMachineLog(tx *gorm.DB) {
	if err := tx.AutoMigrate(&StateMachineLog{}); err != nil {
		panic(err)
	}
}
