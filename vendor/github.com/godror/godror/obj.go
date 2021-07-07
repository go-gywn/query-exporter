// Copyright 2017, 2020 The Godror Authors
//
//
// SPDX-License-Identifier: UPL-1.0 OR Apache-2.0

package godror

/*
#include <stdlib.h>
#include "dpiImpl.h"
*/
import "C"
import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

var _ = fmt.Printf

// Object represents a dpiObject.
type Object struct {
	dpiObject *C.dpiObject
	*ObjectType
}

// ErrNoSuchKey is the error for missing key in lookup.
var ErrNoSuchKey = errors.New("no such key")

// GetAttribute gets the i-th attribute into data.
func (O *Object) GetAttribute(data *Data, name string) error {
	if O == nil || O.dpiObject == nil {
		panic("nil dpiObject")
	}
	attr, ok := O.Attributes[name]
	if !ok {
		return fmt.Errorf("%s: %w", name, ErrNoSuchKey)
	}

	data.reset()
	data.NativeTypeNum = attr.NativeTypeNum
	data.ObjectType = attr.ObjectType
	data.implicitObj = true
	// the maximum length of that buffer must be supplied
	// in the value.asBytes.length attribute before calling this function.
	if attr.NativeTypeNum == C.DPI_NATIVE_TYPE_BYTES && attr.OracleTypeNum == C.DPI_ORACLE_TYPE_NUMBER {
		var a [39]byte
		C.dpiData_setBytes(&data.dpiData, (*C.char)(unsafe.Pointer(&a[0])), C.uint32_t(len(a)))
	}

	//fmt.Printf("getAttributeValue(%p, %p, %d, %+v)\n", O.dpiObject, attr.dpiObjectAttr, data.NativeTypeNum, data.dpiData)
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_getAttributeValue(O.dpiObject, attr.dpiObjectAttr, data.NativeTypeNum, &data.dpiData)
	}); err != nil {
		return fmt.Errorf("getAttributeValue(%q, obj=%+v, attr=%+v, typ=%d): %w", name, O, attr.dpiObjectAttr, data.NativeTypeNum, err)
	}
	if Log != nil {
		Log("msg", "getAttributeValue", "dpiObject", fmt.Sprintf("%p", O.dpiObject),
			attr.Name, fmt.Sprintf("%p", attr.dpiObjectAttr),
			"nativeType", data.NativeTypeNum, "oracleType", attr.OracleTypeNum,
			"data", data.dpiData, "p", fmt.Sprintf("%p", data))
	}
	return nil
}

// SetAttribute sets the named attribute with data.
func (O *Object) SetAttribute(name string, data *Data) error {
	if !strings.Contains(name, `"`) {
		name = strings.ToUpper(name)
	}
	attr := O.Attributes[name]
	if data.NativeTypeNum == 0 {
		data.NativeTypeNum = attr.NativeTypeNum
		data.ObjectType = attr.ObjectType
	}
	return O.conn.checkExec(func() C.int {
		return C.dpiObject_setAttributeValue(O.dpiObject, attr.dpiObjectAttr, data.NativeTypeNum, &data.dpiData)
	})
}

// Set is a convenience function to set the named attribute with the given value.
func (O *Object) Set(name string, v interface{}) error {
	if data, ok := v.(*Data); ok {
		return O.SetAttribute(name, data)
	}
	d := scratch.Get()
	defer scratch.Put(d)
	if err := d.Set(v); err != nil {
		return err
	}
	return O.SetAttribute(name, d)
}

// ResetAttributes prepare all attributes for use the object as IN parameter
func (O *Object) ResetAttributes() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var data Data
	for _, attr := range O.Attributes {
		data.reset()
		data.NativeTypeNum = attr.NativeTypeNum
		data.ObjectType = attr.ObjectType
		if attr.NativeTypeNum == C.DPI_NATIVE_TYPE_BYTES && attr.OracleTypeNum == C.DPI_ORACLE_TYPE_NUMBER {
			a := make([]byte, attr.Precision)
			C.dpiData_setBytes(&data.dpiData, (*C.char)(unsafe.Pointer(&a[0])), C.uint32_t(attr.Precision))
		}
		if C.dpiObject_setAttributeValue(O.dpiObject, attr.dpiObjectAttr, data.NativeTypeNum, &data.dpiData) == C.DPI_FAILURE {
			return O.conn.getError()
		}
	}

	return nil
}

// Get scans the named attribute into dest, and returns it.
func (O *Object) Get(name string) (interface{}, error) {
	d := scratch.Get()
	defer scratch.Put(d)
	if err := O.GetAttribute(d, name); err != nil {
		return nil, err
	}
	isObject := d.IsObject()
	if isObject {
		d.ObjectType = O.Attributes[name].ObjectType
	}
	v := d.Get()
	if !isObject {
		return v, nil
	}
	sub := v.(*Object)
	if sub != nil && sub.CollectionOf != nil {
		return &ObjectCollection{Object: sub}, nil
	}
	return sub, nil
}

// ObjectRef implements userType interface.
func (O *Object) ObjectRef() *Object {
	return O
}

// Collection returns &ObjectCollection{Object: O} iff the Object is a collection.
// Otherwise it returns nil.
func (O *Object) Collection() ObjectCollection {
	if O.ObjectType.CollectionOf == nil {
		return ObjectCollection{}
	}
	return ObjectCollection{Object: O}
}

// Close releases a reference to the object.
func (O *Object) Close() error {
	if O == nil {
		return nil
	}
	obj := O.dpiObject
	O.dpiObject = nil
	if obj == nil {
		return nil
	}
	if Log != nil {
		Log("msg", "Object.Close", "object", obj)
	}
	if err := O.conn.checkExec(func() C.int { return C.dpiObject_release(obj) }); err != nil {
		return fmt.Errorf("error on close object: %w", err)
	}

	return nil
}

// ObjectCollection represents a Collection of Objects - itself an Object, too.
type ObjectCollection struct {
	*Object
}

// ErrNotCollection is returned when the Object is not a collection.
var ErrNotCollection = errors.New("not collection")

// ErrNotExist is returned when the collection's requested element does not exist.
var ErrNotExist = errors.New("not exist")

// AsSlice retrieves the collection into a slice.
func (O ObjectCollection) AsSlice(dest interface{}) (interface{}, error) {
	var dr reflect.Value
	needsInit := dest == nil
	if !needsInit {
		dr = reflect.ValueOf(dest)
	}
	d := scratch.Get()
	defer scratch.Put(d)
	for i, err := O.First(); err == nil; i, err = O.Next(i) {
		if O.CollectionOf.NativeTypeNum == C.DPI_NATIVE_TYPE_OBJECT {
			d.ObjectType = O.CollectionOf
		}
		if err = O.GetItem(d, i); err != nil {
			return dest, err
		}
		vr := reflect.ValueOf(d.Get())
		if needsInit {
			needsInit = false
			length, lengthErr := O.Len()
			if lengthErr != nil {
				return dr.Interface(), lengthErr
			}
			dr = reflect.MakeSlice(reflect.SliceOf(vr.Type()), 0, length)
		}
		dr = reflect.Append(dr, vr)
	}
	return dr.Interface(), nil
}

// AppendData to the collection.
func (O ObjectCollection) AppendData(data *Data) error {
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_appendElement(O.dpiObject, data.NativeTypeNum, &data.dpiData)
	}); err != nil {
		return fmt.Errorf("append(%d): %w", data.NativeTypeNum, err)
	}
	return nil
}

// Append v to the collection.
func (O ObjectCollection) Append(v interface{}) error {
	if data, ok := v.(*Data); ok {
		return O.AppendData(data)
	}
	d := scratch.Get()
	defer scratch.Put(d)
	if err := d.Set(v); err != nil {
		return err
	}
	return O.AppendData(d)
}

// AppendObject adds an Object to the collection.
func (O ObjectCollection) AppendObject(obj *Object) error {
	d := scratch.Get()
	defer scratch.Put(d)
	d.ObjectType = obj.ObjectType
	d.NativeTypeNum = C.DPI_NATIVE_TYPE_OBJECT
	d.SetObject(obj)
	return O.Append(d)
}

// Delete i-th element of the collection.
func (O ObjectCollection) Delete(i int) error {
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_deleteElementByIndex(O.dpiObject, C.int32_t(i))
	}); err != nil {
		return fmt.Errorf("delete(%d): %w", i, err)
	}
	return nil
}

// GetItem gets the i-th element of the collection into data.
func (O ObjectCollection) GetItem(data *Data, i int) error {
	if data == nil {
		panic("data cannot be nil")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	idx := C.int32_t(i)
	var exists C.int
	if C.dpiObject_getElementExistsByIndex(O.dpiObject, idx, &exists) == C.DPI_FAILURE {
		return fmt.Errorf("exists(%d): %w", idx, O.conn.getError())
	}
	if exists == 0 {
		return ErrNotExist
	}
	data.reset()
	data.NativeTypeNum = O.CollectionOf.NativeTypeNum
	data.ObjectType = O.CollectionOf
	data.implicitObj = true
	if C.dpiObject_getElementValueByIndex(O.dpiObject, idx, data.NativeTypeNum, &data.dpiData) == C.DPI_FAILURE {
		return fmt.Errorf("get(%d[%d]): %w", idx, data.NativeTypeNum, O.conn.getError())
	}
	return nil
}

// Get the i-th element of the collection.
func (O ObjectCollection) Get(i int) (interface{}, error) {
	var data Data
	err := O.GetItem(&data, i)
	return data.Get(), err
}

// SetItem sets the i-th element of the collection with data.
func (O ObjectCollection) SetItem(i int, data *Data) error {
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_setElementValueByIndex(O.dpiObject, C.int32_t(i), data.NativeTypeNum, &data.dpiData)
	}); err != nil {
		return fmt.Errorf("set(%d[%d]): %w", i, data.NativeTypeNum, err)
	}
	return nil
}

// Set the i-th element of the collection with value.
func (O ObjectCollection) Set(i int, v interface{}) error {
	if data, ok := v.(*Data); ok {
		return O.SetItem(i, data)
	}
	d := scratch.Get()
	defer scratch.Put(d)
	if err := d.Set(v); err != nil {
		return err
	}
	return O.SetItem(i, d)
}

// First returns the first element's index of the collection.
func (O ObjectCollection) First() (int, error) {
	var exists C.int
	var idx C.int32_t
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_getFirstIndex(O.dpiObject, &idx, &exists)
	}); err != nil {
		return 0, fmt.Errorf("first: %w", err)
	}
	if exists == 1 {
		return int(idx), nil
	}
	return 0, ErrNotExist
}

// Last returns the index of the last element.
func (O ObjectCollection) Last() (int, error) {
	var exists C.int
	var idx C.int32_t
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_getLastIndex(O.dpiObject, &idx, &exists)
	}); err != nil {
		return 0, fmt.Errorf("last: %w", err)
	}
	if exists == 1 {
		return int(idx), nil
	}
	return 0, ErrNotExist
}

// Next returns the succeeding index of i.
func (O ObjectCollection) Next(i int) (int, error) {
	var exists C.int
	var idx C.int32_t
	if err := O.conn.checkExec(func() C.int {
		return C.dpiObject_getNextIndex(O.dpiObject, C.int32_t(i), &idx, &exists)
	}); err != nil {
		return 0, fmt.Errorf("next(%d): %w", i, err)
	}
	if exists == 1 {
		return int(idx), nil
	}
	return 0, ErrNotExist
}

// Len returns the length of the collection.
func (O ObjectCollection) Len() (int, error) {
	var size C.int32_t
	if err := O.conn.checkExec(func() C.int { return C.dpiObject_getSize(O.dpiObject, &size) }); err != nil {
		return 0, fmt.Errorf("len: %w", err)
	}
	return int(size), nil
}

// Trim the collection to n.
func (O ObjectCollection) Trim(n int) error {
	return O.conn.checkExec(func() C.int { return C.dpiObject_trim(O.dpiObject, C.uint32_t(n)) })
}

// ObjectType holds type info of an Object.
type ObjectType struct {
	CollectionOf                        *ObjectType
	Attributes                          map[string]ObjectAttribute
	conn                                *conn
	dpiObjectType                       *C.dpiObjectType
	Schema, Name                        string
	DBSize, ClientSizeInBytes, CharSize int
	mu                                  sync.RWMutex
	OracleTypeNum                       C.dpiOracleTypeNum
	NativeTypeNum                       C.dpiNativeTypeNum
	Precision                           int16
	Scale                               int8
	FsPrecision                         uint8
}

// NewData returns Data for input parameters on Object/ObjectCollection.
func (t *ObjectType) NewData(baseType interface{}, sliceLen, bufSize int) ([]*Data, error) {
	return t.conn.NewData(baseType, sliceLen, bufSize)
}

func (t *ObjectType) String() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// FullName returns the object's name with the schame prepended.
func (t *ObjectType) FullName() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// GetObjectType returns the ObjectType of a name.
//
// The name is uppercased! Because here Oracle seems to be case-sensitive.
// To leave it as is, enclose it in "-s!
func (c *conn) GetObjectType(name string) (*ObjectType, error) {
	if !strings.Contains(name, "\"") {
		name = strings.ToUpper(name)
	}
	if Log != nil {
		Log("msg", "GetObjectType", "name", name)
	}
	cName := C.CString(name)
	defer func() { C.free(unsafe.Pointer(cName)) }()
	objType := (*C.dpiObjectType)(C.malloc(C.sizeof_void))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dpiConn == nil {
		return nil, driver.ErrBadConn
	}
	if err := c.checkExec(func() C.int {
		return C.dpiConn_getObjectType(c.dpiConn, cName, C.uint32_t(len(name)), &objType)
	}); err != nil {
		C.free(unsafe.Pointer(objType))
		if strings.Contains(err.Error(), "DPI-1062: unexpected OCI return value 1041 in function dpiConn_getObjectType") {
			err = fmt.Errorf("getObjectType(%q) conn=%p: %+v: %w", name, c.dpiConn, err, driver.ErrBadConn)
			c.closeNotLocking()
			return nil, err
		}
		return nil, fmt.Errorf("getObjectType(%q) conn=%p: %w", name, c.dpiConn, err)
	}
	t := &ObjectType{conn: c, dpiObjectType: objType}
	err := t.init()
	return t, err
}

// NewObject returns a new Object with ObjectType type.
//
// As with all Objects, you MUST call Close on it when not needed anymore!
func (t *ObjectType) NewObject() (*Object, error) {
	if Log != nil {
		Log("msg", "NewObject", "name", t.Name)
	}
	obj := (*C.dpiObject)(C.malloc(C.sizeof_void))
	t.mu.RLock()
	err := t.conn.checkExec(func() C.int { return C.dpiObjectType_createObject(t.dpiObjectType, &obj) })
	t.mu.RUnlock()
	if err != nil {
		C.free(unsafe.Pointer(obj))
		return nil, err
	}
	O := &Object{ObjectType: t, dpiObject: obj}
	// https://github.com/oracle/odpi/issues/112#issuecomment-524479532
	return O, O.ResetAttributes()
}

// NewCollection returns a new Collection object with ObjectType type.
// If the ObjectType is not a Collection, it returns ErrNotCollection error.
func (t *ObjectType) NewCollection() (ObjectCollection, error) {
	if t.CollectionOf == nil {
		return ObjectCollection{}, ErrNotCollection
	}
	O, err := t.NewObject()
	if err != nil {
		return ObjectCollection{}, err
	}
	return ObjectCollection{Object: O}, nil
}

// Close releases a reference to the object type.
func (t *ObjectType) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	attributes, cof, d := t.Attributes, t.CollectionOf, t.dpiObjectType
	t.Attributes, t.CollectionOf, t.dpiObjectType = nil, nil, nil

	if d == nil {
		return nil
	}

	if cof != nil {
		if err := cof.Close(); err != nil && Log != nil {
			Log("msg", "ObjectType.Close CollectionOf.Close", "name", t.Name, "collectionOf", cof.Name, "error", err)
		}
	}

	for _, attr := range attributes {
		if err := attr.Close(); err != nil && Log != nil {
			Log("msg", "ObjectType.Close attr.Close", "name", t.Name, "attr", attr.Name, "error", err)
		}
	}
	t.conn = nil

	if Log != nil {
		Log("msg", "ObjectType.Close", "name", t.Name)
	}
	if err := t.conn.checkExec(func() C.int { return C.dpiObjectType_release(d) }); err != nil {
		return fmt.Errorf("error on close object type: %w", err)
	}

	return nil
}

func wrapObject(c *conn, objectType *C.dpiObjectType, object *C.dpiObject) (*Object, error) {
	if objectType == nil {
		return nil, errors.New("objectType is nil")
	}
	if err := c.checkExec(func() C.int { return C.dpiObject_addRef(object) }); err != nil {
		return nil, err
	}
	o := &Object{
		ObjectType: &ObjectType{dpiObjectType: objectType, conn: c},
		dpiObject:  object,
	}
	return o, o.init()
}

func (t *ObjectType) init() error {
	if t.conn == nil {
		panic("conn is nil")
	}
	if t.Name != "" && t.Attributes != nil {
		return nil
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t.mu.RLock()
	d := t.dpiObjectType
	t.mu.RUnlock()
	if d == nil {
		return nil
	}
	var info C.dpiObjectTypeInfo
	if C.dpiObjectType_getInfo(d, &info) == C.DPI_FAILURE {
		return fmt.Errorf("%v.getInfo: %w", t, t.conn.getError())
	}
	t.Schema = C.GoStringN(info.schema, C.int(info.schemaLength))
	t.Name = C.GoStringN(info.name, C.int(info.nameLength))
	t.CollectionOf = nil

	numAttributes := int(info.numAttributes)
	if info.isCollection == 1 {
		t.CollectionOf = &ObjectType{conn: t.conn}
		if err := t.CollectionOf.fromDataTypeInfo(info.elementTypeInfo); err != nil {
			return err
		}
		if t.CollectionOf.Name == "" {
			t.CollectionOf.Schema = t.Schema
			t.CollectionOf.Name = t.Name
		}
		if t.CollectionOf.dpiObjectType != nil {
			C.dpiObjectType_addRef(t.CollectionOf.dpiObjectType)
		}
	}
	if numAttributes == 0 {
		t.Attributes = map[string]ObjectAttribute{}
		return nil
	}
	t.Attributes = make(map[string]ObjectAttribute, numAttributes)
	attrs := make([]*C.dpiObjectAttr, numAttributes)
	if C.dpiObjectType_getAttributes(d,
		C.uint16_t(len(attrs)),
		(**C.dpiObjectAttr)(unsafe.Pointer(&attrs[0])),
	) == C.DPI_FAILURE {
		return fmt.Errorf("%v.getAttributes: %w", t, t.conn.getError())
	}
	for i, attr := range attrs {
		var attrInfo C.dpiObjectAttrInfo
		if C.dpiObjectAttr_getInfo(attr, &attrInfo) == C.DPI_FAILURE {
			return fmt.Errorf("%v.attr_getInfo: %w", attr, t.conn.getError())
		}
		if Log != nil {
			Log("i", i, "attrInfo", attrInfo)
		}
		typ := attrInfo.typeInfo
		sub, err := objectTypeFromDataTypeInfo(t.conn, typ)
		if err != nil {
			return err
		}
		objAttr := ObjectAttribute{
			dpiObjectAttr: attr,
			Name:          C.GoStringN(attrInfo.name, C.int(attrInfo.nameLength)),
			ObjectType:    sub,
		}
		//fmt.Printf("%d=%q. typ=%+v sub=%+v\n", i, objAttr.Name, typ, sub)
		t.Attributes[objAttr.Name] = objAttr
	}

	if false {
		runtime.SetFinalizer(t, func(t *ObjectType) { t.Close() })
	}
	return nil
}

func (t *ObjectType) fromDataTypeInfo(typ C.dpiDataTypeInfo) error {
	t.dpiObjectType = typ.objectType

	t.OracleTypeNum = typ.oracleTypeNum
	t.NativeTypeNum = typ.defaultNativeTypeNum
	t.DBSize = int(typ.dbSizeInBytes)
	t.ClientSizeInBytes = int(typ.clientSizeInBytes)
	t.CharSize = int(typ.sizeInChars)
	t.Precision = int16(typ.precision)
	t.Scale = int8(typ.scale)
	t.FsPrecision = uint8(typ.fsPrecision)
	return t.init()
}
func objectTypeFromDataTypeInfo(conn *conn, typ C.dpiDataTypeInfo) (*ObjectType, error) {
	if conn == nil {
		panic("conn is nil")
	}
	if typ.oracleTypeNum == 0 {
		panic("typ is nil")
	}
	t := &ObjectType{conn: conn}
	err := t.fromDataTypeInfo(typ)
	return t, err
}

// ObjectAttribute is an attribute of an Object.
type ObjectAttribute struct {
	*ObjectType
	dpiObjectAttr *C.dpiObjectAttr
	Name          string
}

// Close the ObjectAttribute.
func (A ObjectAttribute) Close() error {
	if A.dpiObjectAttr == nil {
		return nil
	}
	if Log != nil {
		Log("msg", "ObjectAttribute.Close", "name", A.Name)
	}
	if err := A.ObjectType.conn.checkExec(func() C.int { return C.dpiObjectAttr_release(A.dpiObjectAttr) }); err != nil {
		return err
	}
	return A.ObjectType.Close()
}

// GetObjectType returns the ObjectType for the name.
func GetObjectType(ctx context.Context, ex Execer, typeName string) (*ObjectType, error) {
	c, err := getConn(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("getConn for %s: %w", typeName, err)
	}
	return c.GetObjectType(typeName)
}

var scratch = &dataPool{Pool: sync.Pool{New: func() interface{} { return &Data{} }}}

type dataPool struct{ sync.Pool }

func (dp *dataPool) Get() *Data  { return dp.Pool.Get().(*Data) }
func (dp *dataPool) Put(d *Data) { d.reset(); dp.Pool.Put(d) }
