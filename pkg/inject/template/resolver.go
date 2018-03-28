package template

import (
	"bytes"
	"html/template"

	"k8s.io/api/apps/v1beta1"
)

func ResolveTemplate(deployment *v1beta1.Deployment, pathTemplate string) (string, error) {
	pc := PathConfig{Namespace: deployment.Namespace, DeploymentName: deployment.Name, ContainerName: deployment.Spec.Template.Spec.Containers[0].Name}
	tmpl, err := template.New("pathTemplate").Parse(pathTemplate)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, pc)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
