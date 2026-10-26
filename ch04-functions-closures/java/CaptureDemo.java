// Run with bin/jrun or bin/jrun.ps1. No preview features are required here.
// Predict the output before running; checks assert semantics, not object identity.

static class Box {
    int value;
    Box(int value) { this.value = value; }
    int get() { return value; }
}

static class Holder {
    Box current = new Box(10);
}

void check(boolean condition, String message) {
    if (!condition) throw new AssertionError(message);
}

void main() {
    var holder = new Holder();
    IntSupplier bound = holder.current::get;           // Saves the current Box reference.
    IntSupplier deferred = () -> holder.current.get(); // Reads the field at invocation.
    holder.current.value = 11;
    check(bound.getAsInt() == 11 && deferred.getAsInt() == 11,
            "capturing a reference does not copy the object");
    IO.println("mutate object: " + bound.getAsInt() + " " + deferred.getAsInt());

    holder.current = new Box(20);
    check(bound.getAsInt() == 11 && deferred.getAsInt() == 20,
            "method reference and lambda resolve the receiver at different times");
    IO.println("replace receiver: " + bound.getAsInt() + " " + deferred.getAsInt());

    var values = new ArrayList<>(List.of(1));
    IntSupplier first = () -> values.getFirst();
    values.set(0, 9); // Legal: values is effectively final, its object is mutable.
    check(first.getAsInt() == 9, "captured mutable object");
    IO.println("mutable referent: " + first.getAsInt());

    Supplier<Object> lambdaThis = () -> this;
    Supplier<Object> anonymousThis = new Supplier<>() {
        public Object get() { return this; }
    };
    check(lambdaThis.get() == this, "lambda this is the enclosing instance");
    check(anonymousThis.get() == anonymousThis, "anonymous class has its own this");
    IO.println("lambda this is enclosing: " + (lambdaThis.get() == this));
    IO.println("ALL PASS");
}
