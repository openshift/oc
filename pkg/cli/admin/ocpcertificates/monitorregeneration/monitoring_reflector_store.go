package monitorregeneration

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

type objCreateFunc func(obj interface{}, isFirstSync bool)
type objUpdateFunc func(obj, oldObj interface{})
type objDeleteFunc func(obj interface{})

type monitoringStore struct {
	*cache.FakeCustomStore

	addFunc func(obj interface{}, isReplace bool) error

	// map event UIDs to the last resource version we observed, used to skip recording resources
	// we've already recorded.
	processedResourceUIDs map[types.UID]int
	cacheOfNow            map[types.UID]interface{}
}

func newMonitoringStore(
	createHandlers []objCreateFunc,
	updateHandlers []objUpdateFunc,
	deleteHandlers []objDeleteFunc,
) *monitoringStore {
	s := &monitoringStore{
		FakeCustomStore:       &cache.FakeCustomStore{},
		processedResourceUIDs: map[types.UID]int{},
		cacheOfNow:            map[types.UID]interface{}{},
	}

	s.UpdateFunc = func(obj interface{}) error {
		currentUID := uidOf(obj)
		currentResourceVersion := resourceVersionAsInt(obj)
		if s.processedResourceUIDs[currentUID] >= currentResourceVersion {
			return nil
		}

		defer func() {
			s.processedResourceUIDs[currentUID] = currentResourceVersion
			s.cacheOfNow[currentUID] = obj
		}()

		oldObj, ok := s.cacheOfNow[currentUID]
		if !ok {
			fmt.Printf("#### missing object on update for %v\n", currentUID)
			return nil
		}

		for _, updateHandler := range updateHandlers {
			updateHandler(obj, oldObj)
		}

		return nil
	}

	s.addFunc = func(obj interface{}, isFirstSync bool) error {
		currentUID := uidOf(obj)
		currentResourceVersion := resourceVersionAsInt(obj)
		if s.processedResourceUIDs[currentUID] >= currentResourceVersion {
			return nil
		}

		defer func() {
			s.processedResourceUIDs[currentUID] = currentResourceVersion
			s.cacheOfNow[currentUID] = obj
		}()

		for _, createHandler := range createHandlers {
			createHandler(obj, isFirstSync)
		}

		return nil
	}

	s.AddFunc = func(obj interface{}) error {
		return s.addFunc(obj, false)
	}

	s.DeleteFunc = func(obj interface{}) error {
		currentUID := uidOf(obj)
		currentResourceVersion := resourceVersionAsInt(obj)
		if s.processedResourceUIDs[currentUID] >= currentResourceVersion {
			return nil
		}

		// clear values that have been deleted
		defer func() {
			delete(s.processedResourceUIDs, currentUID)
			delete(s.cacheOfNow, currentUID)
		}()

		for _, deleteHandler := range deleteHandlers {
			deleteHandler(obj)
		}

		return nil
	}

	isFirstSync := true
	// ReplaceFunc called when we do our initial list on starting the reflector.
	// This can do adds, updates, and deletes.
	s.ReplaceFunc = func(items []interface{}, rv string) error {
		defer func() {
			isFirstSync = false
		}()

		newUids := map[types.UID]bool{}
		for _, item := range items {
			newUids[uidOf(item)] = true
		}
		deletedUIDs := map[types.UID]bool{}
		for uid := range s.cacheOfNow {
			if !newUids[uid] {
				deletedUIDs[uid] = true
			}
		}

		for _, obj := range items {
			currentUID := uidOf(obj)

			_, oldObjExists := s.cacheOfNow[currentUID]
			switch {
			case oldObjExists:
				s.UpdateFunc(obj)
			case deletedUIDs[currentUID]:
				s.DeleteFunc(obj)
			default:
				s.addFunc(obj, isFirstSync)
			}
		}
		return nil
	}

	return s
}

func resourceVersionAsInt(obj interface{}) int {
	metadata, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	asInt, err := strconv.ParseInt(metadata.GetResourceVersion(), 10, 64)
	if err != nil {
		panic(err)
	}

	return int(asInt)
}

func uidOf(obj interface{}) types.UID {
	metadata, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}
	return metadata.GetUID()
}
