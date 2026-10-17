<?php
// Shared by the app/pdo and app/lifecycle scripts. Not a fixture itself.
//
// Three SQLite files under $FIXTURE_STATE keep the test areas apart, so a
// failure in one cannot lock the others:
//   app.db     data rows (table ev) for transaction and abort cases
//   marks.db   side-effect markers written by shutdown functions, destructors
//              and code after fastcgi_finish_request()
//   persist.db the persistent-connection cases; a leaked lock stays here
//   lock.db    the busy/locked cases

function state_path(string $file): string
{
    return getenv('FIXTURE_STATE') . '/' . $file;
}

function app_db(string $file = 'app.db', array $opts = []): PDO
{
    $db = new PDO('sqlite:' . state_path($file), null, null, $opts + [
        PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
        PDO::ATTR_TIMEOUT => 5,
    ]);
    $db->exec('CREATE TABLE IF NOT EXISTS ev (id INTEGER PRIMARY KEY AUTOINCREMENT, k TEXT NOT NULL, v TEXT)');
    return $db;
}

function marks_db(): PDO
{
    $db = new PDO('sqlite:' . state_path('marks.db'), null, null, [
        PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
        PDO::ATTR_TIMEOUT => 5,
    ]);
    $db->exec('CREATE TABLE IF NOT EXISTS mark (id INTEGER PRIMARY KEY AUTOINCREMENT, k TEXT NOT NULL, v TEXT NOT NULL)');
    return $db;
}

// mark records that code ran, on its own autocommit connection, so it works
// from shutdown functions and after the response is finished.
function mark(string $k, string $v): void
{
    marks_db()->prepare('INSERT INTO mark (k, v) VALUES (?, ?)')->execute([$k, $v]);
}

function marks(string $k): array
{
    $st = marks_db()->prepare('SELECT v FROM mark WHERE k = ? ORDER BY id');
    $st->execute([$k]);
    return $st->fetchAll(PDO::FETCH_COLUMN);
}

function insert_ev(PDO $db, string $k, string $v = ''): void
{
    $db->prepare('INSERT INTO ev (k, v) VALUES (?, ?)')->execute([$k, $v]);
}

// burn_cpu spins until the process used $seconds of CPU time, which is what
// max_execution_time measures on Linux without zend-max-execution-timers.
function burn_cpu(float $seconds): void
{
    $cpu = static function (): float {
        $u = getrusage();
        return $u['ru_utime.tv_sec'] + $u['ru_utime.tv_usec'] / 1e6
            + $u['ru_stime.tv_sec'] + $u['ru_stime.tv_usec'] / 1e6;
    };
    $end = $cpu() + $seconds;
    $x = '';
    while ($cpu() < $end) {
        for ($i = 0; $i < 1000; $i++) {
            $x = sha1($x);
        }
    }
}
