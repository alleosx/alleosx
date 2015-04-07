from __future__ import unicode_literals
from __future__ import absolute_import
import os

import mock
from tests import unittest

from compose.cli import docker_client
from compose.service import ConfigError


class DockerClientTestCase(unittest.TestCase):

    def test_docker_client_no_home(self):
        with mock.patch.dict(os.environ):
            del os.environ['HOME']
            docker_client.docker_client()

    def test_docker_client_no_docker_host(self):
        with mock.patch.dict(os.environ):
            del os.environ['DOCKER_HOST']
            try:
                docker_client.docker_client()
            except ConfigError as e:
                self.assertIn('DOCKER_HOST is not set', e.msg)
            else:
                self.fail('Should have thrown an ConfigError')

    def test_docker_client_with_custom_timeout(self):
        with mock.patch.dict(os.environ):
            os.environ['DOCKER_CLIENT_TIMEOUT'] = timeout = "300"
            client = docker_client.docker_client()
        self.assertEqual(client.timeout, int(timeout))
