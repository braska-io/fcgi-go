<?php
// fastcgi_finish_request(): the response ends, the script goes on.
// Writes "done", finishes, then:
//   ignore=1      ignore_user_abort(true) right after finishing
//   after_ms=N    sleeps N ms
//   echo_after=1  echoes "late" and flushes
//   second=1      calls fastcgi_finish_request() again
// and finally marks "after first=<r1>[ second=<r2>]".
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/../pdo/_db.php';
$q = fixture('app/lifecycle/finish');
$k = $q['k'] ?? '';

echo 'done';
$first = fastcgi_finish_request();
if (($q['ignore'] ?? '') === '1') {
    ignore_user_abort(true);
}
usleep((int) ($q['after_ms'] ?? 0) * 1000);
if (($q['echo_after'] ?? '') === '1') {
    echo 'late';
    flush();
}
$m = 'after first=' . var_export($first, true);
if (($q['second'] ?? '') === '1') {
    $m .= ' second=' . var_export(fastcgi_finish_request(), true);
}
mark($k, $m);
