package browser

import (
	"context"
	"testing"
)

func TestGeneratedGamepadAxesRejectsInvalidReceivers(t *testing.T) {
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(()=>{
    const getter=Object.getOwnPropertyDescriptor(Gamepad.prototype,'axes').get;
    const cases=[{},Gamepad.prototype];
    return JSON.stringify({name:getter.name,source:Function.prototype.toString.call(getter),
      errors:cases.map(receiver=>{try{getter.call(receiver);return null}catch(error){return [error.name,error.message]}})});
  })()`)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"name":"get axes","source":"function get axes() { [native code] }","errors":[["TypeError","Illegal invocation"],["TypeError","Illegal invocation"]]}`
		if value != want {
			t.Fatalf("frozen Chrome 152 Gamepad.axes contract: got %v, want %s", value, want)
		}
	})
}
