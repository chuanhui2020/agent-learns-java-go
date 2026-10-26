// JDK 25, including JEP 513 (final). Run in a fresh JVM for each experiment.
import java.util.ArrayList;
import java.util.List;

public class InitializationDemo {
    static final List<String> events = new ArrayList<>();

    static int record(String event, int value) {
        events.add(event);
        return value;
    }

    static class Lazy {
        static final int CONSTANT = 7;
        static int value = record("Lazy.field", 8);
        static { events.add("Lazy.static"); }
        Lazy() { events.add("Lazy.constructor"); }
    }

    static class Base {
        Base(int value) { events.add("Base(" + value + ")"); }
    }

    static class Child extends Base {
        Child(int value) {
            // JEP 513: validate before super; no access to the unfinished instance.
            if (value <= 0) throw new IllegalArgumentException("value must be positive");
            int doubled = Math.multiplyExact(value, 2);
            events.add("Child.validate");
            super(doubled);
            events.add("Child.body");
        }
    }

    static class Broken {
        static int value = fail();
        static int fail() { throw new IllegalStateException("deliberate initialization failure"); }
    }

    static void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }

    public static void main(String[] args) {
        check(Lazy.CONSTANT == 7 && events.isEmpty(), "constant read must not initialize Lazy");
        System.out.println("constant read: " + events);
        check(Lazy.value == 8, "static field value");
        new Lazy();
        check(events.equals(List.of("Lazy.field", "Lazy.static", "Lazy.constructor")), "class then instance");
        System.out.println("active use: " + events);

        events.clear();
        new Child(3);
        check(events.equals(List.of("Child.validate", "Base(6)", "Child.body")), "constructor order");
        System.out.println("construction: " + events);
        events.clear();
        try {
            new Child(0);
            throw new AssertionError("invalid value should fail");
        } catch (IllegalArgumentException expected) {
            check(events.isEmpty(), "invalid arguments must fail before the base constructor");
        }
        System.out.println("invalid construction: " + events);

        try {
            int unused = Broken.value;
            throw new AssertionError("first use should fail");
        } catch (ExceptionInInitializerError expected) {
            check(expected.getCause() instanceof IllegalStateException, "initial failure cause");
            System.out.println("first failure: " + expected.getClass().getSimpleName());
        }
        try {
            int unused = Broken.value;
            throw new AssertionError("second use should fail");
        } catch (NoClassDefFoundError expected) {
            System.out.println("later failure: " + expected.getClass().getSimpleName());
        }
        System.out.println("ALL PASS");
    }
}
