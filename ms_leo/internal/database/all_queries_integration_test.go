package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Методы, которые этот тест не вызывает: они не про запросы к схеме.
var allQueriesSkip = map[string]string{
	"Close":                  "закрывает соединение",
	"CreateTables":           "уже вызван при подготовке базы",
	"RunMigrations":          "уже вызван при подготовке базы",
	"ApplyMigration":         "меняет схему",
	"RollbackMigration":      "меняет схему",
	"SetAlphaTesterIDs":      "настройка в памяти",
	"SetTrackerDB":           "подмена соединения",
	"SetLogger":              "настройка в памяти",
	"TrackEvent":             "пишет в фоне, проверяется отдельно",
	"AdminRunQuery":          "произвольный SQL из админки",
	"AdminTablePage":         "произвольная таблица из админки",
	"AdminTableColumns":      "произвольная таблица из админки",
	"AnonymizeUserAnalytics": "требует соль и реального пользователя",
}

// Каждый запрос пакета выполняется на настоящей схеме со всеми миграциями.
// Аргументы — заглушки, поэтому «ничего не найдено» и нарушения ограничений
// нормальны. Не нормально одно: ошибка самого запроса — несуществующая
// колонка или таблица, синтаксис, неверное число параметров. Именно так
// сломанный SQL раньше доезжал до прода незамеченным.
func TestEveryQueryIsValidAgainstSchema(t *testing.T) {
	d := openMigratedTestDB(t)

	v := reflect.ValueOf(d)
	typ := v.Type()
	var broken []string
	called := 0
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if _, skip := allQueriesSkip[m.Name]; skip {
			continue
		}
		args, ok := stubArgs(m.Type)
		if !ok {
			continue
		}
		called++
		if err := callWithTimeout(v.Method(i), args); err != nil {
			if reason := schemaErrorReason(err); reason != "" {
				broken = append(broken, fmt.Sprintf("%s: %s", m.Name, reason))
			}
		}
	}
	sort.Strings(broken)
	if len(broken) > 0 {
		t.Errorf("запросы не соответствуют схеме (%d):\n  %s", len(broken), strings.Join(broken, "\n  "))
	}
	if called < 200 {
		t.Errorf("вызвано только %d методов — тест перестал находить методы пакета", called)
	}
	t.Logf("проверено методов: %d", called)
}

// stubArgs собирает безобидные аргументы для метода. ok=false — тип
// аргумента неизвестен, метод пропускаем.
func stubArgs(mt reflect.Type) ([]reflect.Value, bool) {
	args := make([]reflect.Value, 0, mt.NumIn()-1)
	for i := 1; i < mt.NumIn(); i++ {
		a, ok := stubValue(mt.In(i))
		if !ok {
			return nil, false
		}
		args = append(args, a)
	}
	if mt.IsVariadic() {
		args = args[:len(args)-1]
	}
	return args, true
}

var stubTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func stubValue(t reflect.Type) (reflect.Value, bool) {
	switch t {
	case reflect.TypeOf(time.Time{}):
		return reflect.ValueOf(stubTime), true
	case reflect.TypeOf(time.Duration(0)):
		return reflect.ValueOf(time.Hour), true
	case reflect.TypeOf((*context.Context)(nil)).Elem():
		return reflect.ValueOf(context.Background()), true
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int32, reflect.Int64:
		v := reflect.New(t).Elem()
		v.SetInt(1)
		return v, true
	case reflect.String:
		v := reflect.New(t).Elem()
		// Дата годится и туда, где ждут дату, и туда, где ждут просто текст.
		v.SetString("2026-01-15")
		return v, true
	case reflect.Bool, reflect.Float64:
		return reflect.Zero(t), true
	case reflect.Slice:
		elem, ok := stubValue(t.Elem())
		if !ok {
			return reflect.Zero(t), true
		}
		return reflect.Append(reflect.MakeSlice(t, 0, 1), elem), true
	case reflect.Map:
		return reflect.MakeMap(t), true
	case reflect.Struct:
		return reflect.Zero(t), true
	case reflect.Ptr:
		if t.Elem().Kind() == reflect.Struct {
			return reflect.New(t.Elem()), true
		}
		return reflect.Zero(t), true
	case reflect.Interface, reflect.Func, reflect.Chan:
		return reflect.Value{}, false
	}
	return reflect.Value{}, false
}

// callWithTimeout вызывает метод и возвращает его ошибку. Паника на заглушках
// и зависание не считаются ошибкой запроса.
func callWithTimeout(method reflect.Value, args []reflect.Value) (err error) {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- nil
			}
		}()
		var out error
		for _, r := range method.Call(args) {
			if e, ok := r.Interface().(error); ok && e != nil {
				out = e
			}
		}
		done <- out
	}()
	select {
	case err = <-done:
		return err
	case <-time.After(20 * time.Second):
		return nil
	}
}

// schemaErrorReason — текст, если ошибка говорит о несоответствии запроса
// схеме (класс 42 в Postgres: синтаксис, нет колонки/таблицы/функции,
// неоднозначность) или о неверном числе параметров. Иначе пусто.
func schemaErrorReason(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if pqErr.Code.Class() == "42" {
			return fmt.Sprintf("%s (%s)", pqErr.Message, pqErr.Code)
		}
		// 08P01: число параметров запроса не совпало с переданными.
		if pqErr.Code == "08P01" {
			return pqErr.Message
		}
		return ""
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	msg := err.Error()
	for _, sign := range []string{"does not exist", "syntax error", "expected", "destination arguments", "converting argument"} {
		if strings.Contains(msg, sign) && (strings.Contains(msg, "pq:") || strings.Contains(msg, "sql:")) {
			return msg
		}
	}
	return ""
}
