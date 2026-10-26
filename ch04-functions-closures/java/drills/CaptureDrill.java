// Implement the two TODOs. Initially fails with exit code 1; keep checks unchanged.
// Run with bin/jrun or bin/jrun.ps1.

/** Return n independent counters, each returning 1 on its first call; n <= 0 is empty. */
IntSupplier[] makeCounters(int n) {
    // TODO: implement with lambdas. Why can a captured object's state change?
    return new IntSupplier[0];
}

static class Box {
    int value;
    Box(int value) { this.value = value; }
    int get() { return value; }
}

static class Holder {
    Box current = new Box(10);
}

/** Predict [bound after mutation, deferred after mutation, bound after replacement, deferred after replacement]. */
List<Integer> predictReceivers() {
    // TODO: read receiverResults before running and fill in your prediction.
    return List.of();
}

List<Integer> receiverResults() {
    var holder = new Holder();
    IntSupplier bound = holder.current::get;
    IntSupplier deferred = () -> holder.current.get();
    holder.current.value = 12;
    var values = new ArrayList<Integer>();
    values.add(bound.getAsInt());
    values.add(deferred.getAsInt());
    holder.current = new Box(30);
    values.add(bound.getAsInt());
    values.add(deferred.getAsInt());
    return values;
}

// ---------------- Self-checks: do not edit ----------------
int failures;

void check(boolean condition, String message) {
    if (!condition) {
        failures++;
        IO.println("FAIL " + message);
    }
}

void main() {
    for (int n : new int[]{-1, 0, 1, 3}) {
        var counters = makeCounters(n);
        check(counters != null && counters.length == Math.max(n, 0), "counter count for n=" + n);
        if (counters == null || counters.length != Math.max(n, 0)) continue;
        for (int i = 0; i < counters.length; i++) {
            check(counters[i] != null, "counter " + i + " is callable");
            if (counters[i] == null) continue;
            for (int want = 1; want <= i + 2; want++) {
                check(counters[i].getAsInt() == want, "independent counter " + i + ", call " + want);
            }
        }
        for (int i = counters.length - 1; i >= 0; i--) {
            if (counters[i] != null) check(counters[i].getAsInt() == i + 3, "retained state " + i);
        }
    }
    check(receiverResults().equals(predictReceivers()), "receiver prediction");
    IO.println(failures == 0 ? "ALL PASS" : failures + " FAILED");
    if (failures > 0) System.exit(1);
}
