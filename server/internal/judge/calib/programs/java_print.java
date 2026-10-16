// Hostile: prints forever. The JVM ignores SIGXFSZ, so only the output
// size reveals that it hit the cap.
public class Main {
    public static void main(String[] args) {
        StringBuilder line = new StringBuilder();
        for (int i = 0; i < 8; i++) line.append("podium ");
        String s = line.toString();
        while (true) System.out.println(s);
    }
}
