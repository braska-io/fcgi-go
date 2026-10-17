<?php
// A large result set written row by row. Rows are "<i>:<64 x 'r'>\n".
//   mode=stream  fetch() one row at a time, echo and flush every 1000 rows,
//                then a trailer "rows=<n> bytes=<b> peak_under_16m=<0|1>"
//   mode=all     fetchAll() the same rows first, which needs more than the
//                64M memory_limit for rows=200000
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/stream');
$rows = (int) ($q['rows'] ?? 1000);

$db = app_db();
$db->exec('CREATE TEMP TABLE big (i INTEGER PRIMARY KEY, pad TEXT NOT NULL)');
$db->beginTransaction();
$ins = $db->prepare('INSERT INTO big (i, pad) VALUES (?, ?)');
$pad = str_repeat('r', 64);
for ($i = 0; $i < $rows; $i++) {
    $ins->execute([$i, $pad]);
}
$db->commit();

$st = $db->query('SELECT i, pad FROM big ORDER BY i');
if (($q['mode'] ?? 'stream') === 'all') {
    $all = $st->fetchAll(PDO::FETCH_ASSOC);
    echo 'fetched ' . count($all);
    exit;
}

$n = 0;
$bytes = 0;
while (($r = $st->fetch(PDO::FETCH_NUM)) !== false) {
    $line = $r[0] . ':' . $r[1] . "\n";
    echo $line;
    $bytes += strlen($line);
    if (++$n % 1000 === 0) {
        flush();
    }
}
echo 'rows=' . $n . ' bytes=' . $bytes . ' peak_under_16m=' . (memory_get_peak_usage() < 16 << 20 ? '1' : '0');
