import unittest
from pathlib import Path
from unittest.mock import patch

from windows_tests import (BROWSER, CDP, NAVIGATION_GATE, ROOT, batches,
                           load_timings, package_batches, partition, root_tests,
                           test_command)


class ShardCoverageTests(unittest.TestCase):
    def test_every_root_test_appears_in_exactly_one_shard(self):
        names = ['Test' + str(index) for index in range(661)] + ['ExamplePage', 'FuzzParser']
        for count in (2, 3, 4):
            with self.subTest(count=count):
                shards = [partition(names, index, count) for index in range(count)]
                for left in range(count):
                    for right in range(left + 1, count):
                        self.assertFalse(set(shards[left]) & set(shards[right]))
                self.assertCountEqual(sum(shards, []), names)
                self.assertEqual(partition(list(reversed(names)), 0, count), shards[0])

    def test_weighted_partition_balances_long_tests(self):
        names = ['TestSlowA', 'TestSlowB', 'TestFastA', 'TestFastB']
        timings = {'TestSlowA': 10, 'TestSlowB': 9, 'TestFastA': 1, 'TestFastB': 1}
        shards = [partition(names, index, 2, timings) for index in range(2)]
        self.assertNotEqual(
            next(index for index, shard in enumerate(shards) if 'TestSlowA' in shard),
            next(index for index, shard in enumerate(shards) if 'TestSlowB' in shard),
        )
        self.assertCountEqual(sum(shards, []), names)

    def test_unknown_tests_receive_heaviest_known_weight(self):
        names = ['TestKnown', 'TestUnknownA', 'TestUnknownB']
        shards = [partition(names, index, 2, {'TestKnown': 8}) for index in range(2)]
        self.assertCountEqual(sum(shards, []), names)

    def test_bad_shard_and_duplicate_discovery_fail(self):
        for names, shard, count in [(['TestA'], 2, 2), (['TestA'], 0, 0),
                                     (['TestA', 'TestA'], 0, 2)]:
            with self.subTest(names=names, shard=shard, count=count), self.assertRaises(ValueError):
                partition(names, shard, count)

    def test_process_batches_cover_each_root_and_its_subtests(self):
        names = ['Test' + str(index) for index in range(85)]
        groups = list(batches(names, 10))
        self.assertEqual([len(group) for group in groups], [10] * 8 + [5])
        self.assertEqual(sum(groups, []), names)
        self.assertEqual(list(batches([], 10)), [])
        with self.assertRaises(ValueError):
            list(batches(names, 0))

    def test_browser_and_cdp_names_remain_distinct_and_complete(self):
        names = [package + '::TestSharedName' for package in (BROWSER, CDP)]
        names += [CDP + '::' + NAVIGATION_GATE]
        shards = [partition(names, index, 3) for index in range(3)]
        self.assertCountEqual(sum(shards, []), names)
        self.assertEqual(len(set(sum(shards, []))), len(names))

    def test_navigation_gate_has_its_own_process(self):
        names = ['TestA', NAVIGATION_GATE, 'TestB']
        self.assertEqual(list(package_batches(CDP, names)),
                         [['TestA', 'TestB'], [NAVIGATION_GATE]])
        self.assertEqual(list(package_batches(BROWSER, names)), [names])

    def test_precompiled_tests_discover_and_execute_in_package_directory(self):
        directory = Path('compiled tests')
        with patch('windows_tests.run', return_value='TestA\nExampleB\n') as run:
            self.assertEqual(root_tests(CDP, directory), ['TestA', 'ExampleB'])
            self.assertEqual(run.call_args.kwargs['cwd'], ROOT / 'internal/cdp')
            self.assertEqual(run.call_args.args[1], '-test.list=.')
        command, cwd = test_command(CDP, ['TestA'], directory)
        self.assertEqual(cwd, ROOT / 'internal/cdp')
        self.assertEqual(command[:6], ['go', 'tool', 'test2json', '-t', '-p', CDP])
        self.assertIn('-test.v=test2json', command)
        self.assertIn('-test.timeout=10m', command)
        self.assertIn('-test.run=^(TestA)$', command)


if __name__ == '__main__':
    unittest.main()
