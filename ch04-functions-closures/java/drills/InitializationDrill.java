// Fill in predictions before running. Initially fails; do not edit the experiment.
import java.util.ArrayList;
import java.util.List;

public class InitializationDrill {
    static List<String> predictConstantRead() {
        // TODO: predict the complete event list after reading Lazy.CONSTANT.
        return null;
    }

    static List<String> predictConstruction() {
        // TODO: predict the complete event list after new Child(Lazy.value).
        return null;
    }

    // ---------------- Experiment and self-checks: do not edit ----------------
    static final List<String> events = new ArrayList<>();

    static class Lazy {
        static final int CONSTANT = 4;
        static int value = initialize();
        static int initialize() {
            events.add("Lazy.field");
            return 4;
        }
        static { events.add("Lazy.static"); }
    }

    static class Base {
        Base(int n) { events.add("Base(" + n + ")"); }
    }

    static class Child extends Base {
        Child(int n) {
            if (n <= 0) throw new IllegalArgumentException();
            events.add("Child.validate");
            super(n + 1);
            events.add("Child.body");
        }
    }

    public static void main(String[] args) {
        var constantPrediction = predictConstantRead();
        var constructionPrediction = predictConstruction();
        int failures = 0;
        int constant = Lazy.CONSTANT;
        if (!events.equals(constantPrediction)) {
            failures++;
            System.out.println("FAIL constant-read prediction");
        }
        new Child(Lazy.value);
        if (!events.equals(constructionPrediction)) {
            failures++;
            System.out.println("FAIL construction prediction");
        }
        System.out.println(failures == 0 ? "ALL PASS" : failures + " FAILED");
        if (failures > 0) System.exit(1);
    }
}
