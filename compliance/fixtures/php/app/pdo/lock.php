<?php
// A write lock held by one request against parallel requests on lock.db.
//   op=hold&ms=N                       BEGIN IMMEDIATE, sleep N ms, commit
//   op=write&delay_ms=D&timeout=S      sleep D ms, then BEGIN IMMEDIATE with
//                                      PDO::ATTR_TIMEOUT = S seconds
//   op=read&delay_ms=D                 sleep D ms, then a plain SELECT
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/lock');

$timeout = (int) ($q['timeout'] ?? 5);
$db = app_db('lock.db', [PDO::ATTR_TIMEOUT => $timeout]);
usleep((int) ($q['delay_ms'] ?? 0) * 1000);

switch ($q['op'] ?? '') {
case 'hold':
    $db->exec('BEGIN IMMEDIATE');
    insert_ev($db, 'hold');
    usleep((int) ($q['ms'] ?? 0) * 1000);
    $db->exec('COMMIT');
    echo 'held';
    break;
case 'write':
    try {
        $db->exec('BEGIN IMMEDIATE');
        insert_ev($db, 'write');
        $db->exec('COMMIT');
        echo 'acquired';
    } catch (PDOException $e) {
        // errorInfo[1] is the driver code; SQLITE_BUSY is 5.
        echo 'busy:' . ($e->errorInfo[1] ?? '?');
    }
    break;
case 'read':
    echo 'read:' . ($db->query('SELECT COUNT(*) >= 0 FROM ev')->fetchColumn());
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
