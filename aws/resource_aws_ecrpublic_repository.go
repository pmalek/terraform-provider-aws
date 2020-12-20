package aws

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecrpublic"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func resourceAwsEcrPublicRepository() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsEcrPublicRepositoryCreate,
		Read:   resourceAwsEcrPublicRepositoryRead,
		Update: resourceAwsEcrPublicUpdate,
		Delete: resourceAwsEcrPublicDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Delete: schema.DefaultTimeout(20 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"repository_name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.All(
					validation.StringLenBetween(2, 205),
					validation.StringMatch(regexp.MustCompile(`(?:[a-z0-9]+(?:[._-][a-z0-9]+)*/)*[a-z0-9]+(?:[._-][a-z0-9]+)*`), "see: https://docs.aws.amazon.com/AmazonECRPublic/latest/APIReference/API_CreateRepository.html#API_CreateRepository_RequestSyntax"),
				),
			},
			"catalog_data": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"about_text": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringLenBetween(0, 10240),
						},
						"architectures": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 50,
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
						},
						"created_at": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"description": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringLenBetween(0, 1024),
						},
						"logo_image_blob": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"logo_image_url": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"operating_systems": {
							Type:     schema.TypeList,
							Optional: true,
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
							ValidateFunc: validation.StringInSlice([]string{
								"ARM",
								"ARM 64",
								"x86",
								"x86-64",
							}, false),
						},
						"registry_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"repository_arn": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"repository_uri": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"usage_text": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringLenBetween(0, 10240),
						},
					},
				},
				DiffSuppressFunc: suppressMissingOptionalConfigurationBlock,
				ForceNew:         true,
			},
		},
	}
}

func resourceAwsEcrPublicRepositoryCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecrpublicconn

	input := ecrpublic.CreateRepositoryInput{
		RepositoryName: aws.String(d.Get("name").(string)),
	}

	if v, ok := d.GetOk("catalog_data"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		input.CatalogData = expandEcrPublicRepositoryCatalogData(v.([]interface{})[0].(map[string]interface{}))
	}

	log.Printf("[DEBUG] Creating ECR Public repository: %#v", input)
	out, err := conn.CreateRepository(&input)
	if err != nil {
		return fmt.Errorf("error creating ECR Public repository: %s", err)
	}

	repository := *out.Repository

	log.Printf("[DEBUG] ECR Public repository created: %q", *repository.RepositoryArn)

	d.SetId(aws.StringValue(repository.RepositoryName))

	return resourceAwsEcrPublicRepositoryRead(d, meta)
}

func resourceAwsEcrPublicRepositoryRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecrpublicconn

	log.Printf("[DEBUG] Reading ECR PUblic repository %s", d.Id())
	var out *ecrpublic.DescribeRepositoriesOutput
	input := &ecrpublic.DescribeRepositoriesInput{
		RepositoryNames: aws.StringSlice([]string{d.Id()}),
	}

	var err error
	err = resource.Retry(1*time.Minute, func() *resource.RetryError {
		out, err = conn.DescribeRepositories(input)
		if d.IsNewResource() && isAWSErr(err, ecrpublic.ErrCodeRepositoryNotFoundException, "") {
			return resource.RetryableError(err)
		}
		if err != nil {
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if isResourceTimeoutError(err) {
		out, err = conn.DescribeRepositories(input)
	}

	if isAWSErr(err, ecrpublic.ErrCodeRepositoryNotFoundException, "") {
		log.Printf("[WARN] ECR Public Repository (%s) not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	if err != nil {
		return fmt.Errorf("error reading ECR Public repository: %s", err)
	}

	repository := out.Repositories[0]

	d.Set("repository_name", d.Id())
	d.Set("created_at", aws.TimeValue(repository.CreatedAt).Format(time.RFC3339))
	d.Set("registry_id", repository.RegistryId)
	d.Set("repository_arn", repository.RepositoryArn)
	d.Set("repository_uri", repository.RepositoryUri)

	var catalogOut *ecrpublic.GetRepositoryCatalogDataOutput
	catalogInput := &ecrpublic.GetRepositoryCatalogDataInput{
		RepositoryName: aws.String(d.Id()),
		RegistryId:     repository.RegistryId,
	}

	// not sure about error handling for this second call
	catalogOut, err = conn.GetRepositoryCatalogData(catalogInput)

	if catalogOut != nil {
		d.Set("catalog_data", []interface{}{flattenEcrPublicRepositoryCatalogData(catalogOut)})
	} else {
		d.Set("catalog_data", nil)
	}

	return nil
}

func resourceAwsEcrPublicRepositoryDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecrpublicconn

	_, err := conn.DeleteRepository(&ecrpublic.DeleteRepositoryInput{
		RepositoryName: aws.String(d.Id()),
		RegistryId:     aws.String(d.Get("registry_id").(string)),
		Force:          aws.Bool(true),
	})
	if err != nil {
		if isAWSErr(err, ecrpublic.ErrCodeRepositoryNotFoundException, "") {
			return nil
		}
		return fmt.Errorf("error deleting ECR Public repository: %s", err)
	}

	log.Printf("[DEBUG] Waiting for ECR Public Repository %q to be deleted", d.Id())
	input := &ecrpublic.DescribeRepositoriesInput{
		RepositoryNames: aws.StringSlice([]string{d.Id()}),
	}
	err = resource.Retry(d.Timeout(schema.TimeoutDelete), func() *resource.RetryError {
		_, err = conn.DescribeRepositories(input)
		if err != nil {
			if isAWSErr(err, ecrpublic.ErrCodeRepositoryNotFoundException, "") {
				return nil
			}
			return resource.NonRetryableError(err)
		}

		return resource.RetryableError(fmt.Errorf("%q: Timeout while waiting for the ECR Public Repository to be deleted", d.Id()))
	})
	if isResourceTimeoutError(err) {
		_, err = conn.DescribeRepositories(input)
	}

	if isAWSErr(err, ecrpublic.ErrCodeRepositoryNotFoundException, "") {
		return nil
	}

	if err != nil {
		return fmt.Errorf("error deleting ECR Public repository: %s", err)
	}

	log.Printf("[DEBUG] repository %q deleted.", d.Get("name").(string))

	return nil
}

func flattenEcrPublicRepositoryCatalogData(apiObject *ecrpublic.GetRepositoryCatalogDataOutput) map[string]interface{} {
	if apiObject == nil {
		return nil
	}

	catalogData := apiObject.CatalogData

	tfMap := map[string]interface{}{}

	if v := catalogData.AboutText; v != nil {
		tfMap["about_text"] = aws.StringValue(v)
	}

	if v := catalogData.Architectures; v != nil {
		tfMap["architectures"] = aws.StringValueSlice(v)
	}

	if v := catalogData.Description; v != nil {
		tfMap["description"] = aws.StringValue(v)
	}

	if v := catalogData.LogoUrl; v != nil {
		tfMap["logo_image_url"] = aws.StringValue(v)
	}

	if v := catalogData.OperatingSystems; v != nil {
		tfMap["operating_systems"] = aws.StringValueSlice(v)
	}

	if v := catalogData.UsageText; v != nil {
		tfMap["usage_text"] = aws.StringValue(v)
	}

	return tfMap
}

func expandEcrPublicRepositoryCatalogData(tfMap map[string]interface{}) *ecrpublic.RepositoryCatalogDataInput {
	if tfMap == nil {
		return nil
	}

	repositoryCatalogDataInput := &ecrpublic.RepositoryCatalogDataInput{}

	if v, ok := tfMap["about_text"].(string); ok && v != "" {
		repositoryCatalogDataInput.AboutText = aws.String(v)
	}

	if v, ok := tfMap["architectures"].([]interface{}); ok && len(v) > 0 {
		repositoryCatalogDataInput.Architectures = expandStringList(v)
	}

	if v, ok := tfMap["logo_image_blob"].([]byte); ok && len(v) > 0 {
		repositoryCatalogDataInput.LogoImageBlob = v
	}

	if v, ok := tfMap["operating_systems"].([]interface{}); ok && len(v) > 0 {
		repositoryCatalogDataInput.OperatingSystems = expandStringList(v)
	}

	if v, ok := tfMap["usage_text"].(string); ok && v != "" {
		repositoryCatalogDataInput.AboutText = aws.String(v)
	}

	return repositoryCatalogDataInput
}