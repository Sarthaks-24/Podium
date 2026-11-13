// Hostile: starts threads that sleep until the process limit stops it.
public class Main {
    public static void main(String[] args) {
        while (true) {
            new Thread(() -> {
                try {
                    Thread.sleep(600000);
                } catch (InterruptedException e) {
                }
            }).start();
        }
    }
}
