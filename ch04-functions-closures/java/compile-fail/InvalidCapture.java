// Expected compilation failure: the captured local variable is not effectively final.
import java.util.function.IntSupplier;

class InvalidCapture {
    IntSupplier create() {
        int value = 1;
        IntSupplier read = () -> value;
        value++;
        return read;
    }
}
