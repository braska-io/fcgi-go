<?php
// Persistent connections. php-fpm keeps one persistent handle per child, and
// the suite cannot pick the child, so each hit records "<pid>:<n>" where n
// counts this key's rows in a connection-scoped TEMP table. verify then
// checks the per-child sequences: on a reused connection n grows by one per
// hit of that child; on a fresh connection n is always 1. With more hits than
// the pool has children (4), at least one child serves two hits.
//   op=hit&k=K&mode=persistent|fresh|late   late: ATTR_PERSISTENT via setAttribute
//   op=verify&k=K                           body: reused | fresh | diagnostic
//   op=is_persistent&mode=...               getAttribute(ATTR_PERSISTENT)
//   op=open_txn&k=K                         beginTransaction + insert, no commit
//   op=insert&k=K                           autocommit insert
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/persist');
$k = $q['k'] ?? '';
$mode = $q['mode'] ?? 'persistent';

function connect(string $mode): PDO
{
    $opts = $mode === 'persistent' ? [PDO::ATTR_PERSISTENT => true] : [];
    $db = app_db('persist.db', $opts);
    if ($mode === 'late') {
        try {
            $db->setAttribute(PDO::ATTR_PERSISTENT, true);
        } catch (PDOException $e) {
            // The driver may refuse the attribute; either way the doc says
            // the connection is not persistent.
        }
    }
    return $db;
}

switch ($q['op'] ?? '') {
case 'hit':
    $db = connect($mode);
    $db->exec('CREATE TEMP TABLE IF NOT EXISTS hit (k TEXT NOT NULL)');
    $db->prepare('INSERT INTO temp.hit (k) VALUES (?)')->execute([$k]);
    $st = $db->prepare('SELECT COUNT(*) FROM temp.hit WHERE k = ?');
    $st->execute([$k]);
    mark($k, getmypid() . ':' . $st->fetchColumn());
    echo 'hit';
    break;
case 'verify':
    $seq = [];
    foreach (marks($k) as $m) {
        [$pid, $n] = explode(':', $m);
        $seq[$pid][] = (int) $n;
    }
    $reused = false;
    $allOne = true;
    $consecutive = true;
    foreach ($seq as $ns) {
        $reused = $reused || count($ns) > 1;
        foreach ($ns as $i => $n) {
            $allOne = $allOne && $n === 1;
            if ($i > 0 && $n !== $ns[$i - 1] + 1) {
                $consecutive = false;
            }
        }
    }
    if (!$reused) {
        echo 'no child served two hits';
    } elseif ($allOne) {
        echo 'fresh';
    } elseif ($consecutive) {
        echo 'reused';
    } else {
        echo 'mixed ' . json_encode($seq);
    }
    break;
case 'is_persistent':
    echo var_export((bool) connect($mode)->getAttribute(PDO::ATTR_PERSISTENT), true);
    break;
case 'open_txn':
    $db = connect($mode);
    $db->beginTransaction();
    insert_ev($db, $k);
    echo 'left open';
    break;
case 'insert':
    insert_ev(connect($mode), $k);
    echo 'inserted';
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
