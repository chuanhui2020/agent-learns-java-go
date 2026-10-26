// Expected compilation failure: calling an instance method before super(...).
class InvalidConstruction {
    int read() { return 1; }

    InvalidConstruction() {
        int value = read();
        super();
    }
}
