<?php
// A transaction that runs while the gateway gives up on the call.
// output_buffering = 4096 holds small output in PHP's own buffer, and flush()
// does not touch that buffer, so each write is ob_flush() + flush().
//   mode=ignore   ignore_user_abort(true); write "." and flush every 100 ms
//                 for up to 5 s; on connection_aborted() roll back and mark
//                 "aborted", else commit and mark "completed"
//   mode=default  the same loop without ignore_user_abort; a shutdown
//                 function marks "shutdown aborted=<connection_aborted()>"
//   mode=quiet    ignore_user_abort(true); sleep sleep_ms without writing,
//                 commit, mark "committed aborted=<connection_aborted()>"
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/../pdo/_db.php';
$q = fixture('app/lifecycle/abort');
$k = $q['k'] ?? '';
$mode = $q['mode'] ?? 'ignore';

if ($mode !== 'default') {
    ignore_user_abort(true);
} else {
    register_shutdown_function(function () use ($k) {
        mark($k, 'shutdown aborted=' . connection_aborted());
    });
}

$db = app_db();
$db->beginTransaction();
insert_ev($db, $k);

if ($mode === 'quiet') {
    usleep((int) ($q['sleep_ms'] ?? 0) * 1000);
    $db->commit();
    mark($k, 'committed aborted=' . connection_aborted());
    echo 'done';
    exit;
}

for ($i = 0; $i < 50; $i++) {
    usleep(100 * 1000);
    echo '.';
    ob_flush();
    flush();
    if (connection_aborted()) {
        $db->rollBack();
        mark($k, 'aborted');
        exit;
    }
}
$db->commit();
mark($k, 'completed');
