<?php
// A typical application page for a signed-in user: the session the client's
// cookie names, one simulated database round trip,
// some data shaping and a rendered HTML response of about 60 KiB.
session_start();
$_SESSION['views'] = ($_SESSION['views'] ?? 0) + 1;
usleep(5000);

$rows = [];
for ($i = 0; $i < 400; $i++) {
    $rows[] = ['id' => $i, 'name' => "item $i", 'price' => $i * 1.07, 'tags' => ['a', 'b', 'c']];
}
usort($rows, fn($a, $b) => $b['price'] <=> $a['price']);
$json = json_encode($rows);

header('Content-Type: text/html; charset=utf-8');
header('Cache-Control: no-store');
echo "<!doctype html><html><body><table>\n";
foreach ($rows as $r) {
    printf("<tr><td>%d</td><td>%s</td><td>%.2f</td><td>%s</td></tr>\n",
        $r['id'], htmlspecialchars($r['name']), $r['price'], implode(',', $r['tags']));
}
echo "</table><script>const data = $json;</script></body></html>\n";
